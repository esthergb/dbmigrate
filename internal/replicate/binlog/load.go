package binlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	goMySQL "github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/esthergb/dbmigrate/internal/db"
)

type streamEventKind string

const (
	streamEventQuery      streamEventKind = "query"
	streamEventXID        streamEventKind = "xid"
	streamEventWriteRows  streamEventKind = "write_rows"
	streamEventUpdateRows streamEventKind = "update_rows"
	streamEventDeleteRows streamEventKind = "delete_rows"
)

type streamEvent struct {
	Kind   streamEventKind
	File   string
	Pos    uint32
	Schema string
	Table  string
	Query  string
	Rows   [][]any
}

type tableMetadata struct {
	Columns     []string
	KeyOrdinals []int
}

var (
	streamWindowEventsFn = streamWindowEvents
	loadTableMetadataFn  = loadTableMetadata
)

func loadApplyBatchesFromSource(ctx context.Context, source *sql.DB, window applyWindow, opts Options) ([]applyBatch, error) {
	if strings.TrimSpace(opts.SourceDSN) == "" {
		return nil, nil
	}
	if !positionBefore(window.StartFile, window.StartPos, window.EndFile, window.EndPos) {
		return nil, nil
	}

	events, err := streamWindowEventsFn(ctx, window, opts)
	if err != nil {
		return nil, fmt.Errorf("stream source binlog events: %w", err)
	}
	if len(events) == 0 {
		return nil, nil
	}

	batches, err := buildApplyBatches(ctx, source, events, opts)
	if err != nil {
		return nil, err
	}
	return batches, nil
}

func streamWindowEvents(ctx context.Context, window applyWindow, opts Options) ([]streamEvent, error) {
	syncCfg, err := sourceSyncerConfig(opts.SourceDSN)
	if err != nil {
		return nil, err
	}

	syncer := replication.NewBinlogSyncer(syncCfg)
	defer syncer.Close()

	streamer, err := syncer.StartSync(goMySQL.Position{
		Name: window.StartFile,
		Pos:  window.StartPos,
	})
	if err != nil {
		return nil, fmt.Errorf("start source binlog sync: %w", err)
	}

	currentFile := window.StartFile
	events := make([]streamEvent, 0, 256)
	for {
		event, err := streamer.GetEvent(ctx)
		if err != nil {
			return nil, fmt.Errorf("read source binlog event: %w", err)
		}
		if event == nil || event.Header == nil {
			continue
		}

		eventFile := currentFile
		eventPos := event.Header.LogPos
		if event.Header.EventType == replication.ROTATE_EVENT {
			rotate, ok := event.Event.(*replication.RotateEvent)
			if !ok {
				return nil, errors.New("unexpected rotate event payload")
			}
			nextFile := strings.TrimSpace(string(rotate.NextLogName))
			if nextFile != "" {
				currentFile = nextFile
			}
			if positionReachedOrPassed(currentFile, uint32(rotate.Position), window.EndFile, window.EndPos) {
				break
			}
			continue
		}

		if positionAfter(eventFile, eventPos, window.EndFile, window.EndPos) {
			break
		}

		converted, include, err := convertReplicationEvent(event, eventFile)
		if err != nil {
			return nil, err
		}
		if include {
			events = append(events, converted)
		}

		if positionReachedOrPassed(eventFile, eventPos, window.EndFile, window.EndPos) {
			break
		}
	}

	return events, nil
}

func convertReplicationEvent(event *replication.BinlogEvent, file string) (streamEvent, bool, error) {
	switch event.Header.EventType {
	case replication.QUERY_EVENT:
		query, ok := event.Event.(*replication.QueryEvent)
		if !ok {
			return streamEvent{}, false, errors.New("unexpected query event payload")
		}
		return streamEvent{
			Kind:   streamEventQuery,
			File:   file,
			Pos:    event.Header.LogPos,
			Schema: string(query.Schema),
			Query:  strings.TrimSpace(string(query.Query)),
		}, true, nil
	case replication.XID_EVENT:
		return streamEvent{
			Kind: streamEventXID,
			File: file,
			Pos:  event.Header.LogPos,
		}, true, nil
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		return convertRowsEvent(streamEventWriteRows, event, file)
	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		return convertRowsEvent(streamEventUpdateRows, event, file)
	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		return convertRowsEvent(streamEventDeleteRows, event, file)
	default:
		return streamEvent{}, false, nil
	}
}

func convertRowsEvent(kind streamEventKind, event *replication.BinlogEvent, file string) (streamEvent, bool, error) {
	rowsEvent, ok := event.Event.(*replication.RowsEvent)
	if !ok {
		return streamEvent{}, false, errors.New("unexpected rows event payload")
	}
	if rowsEvent.Table == nil {
		return streamEvent{}, false, errors.New("rows event missing table metadata")
	}

	rows := make([][]any, 0, len(rowsEvent.Rows))
	for _, row := range rowsEvent.Rows {
		rowCopy := make([]any, len(row))
		copy(rowCopy, row)
		rows = append(rows, rowCopy)
	}

	return streamEvent{
		Kind:   kind,
		File:   file,
		Pos:    event.Header.LogPos,
		Schema: string(rowsEvent.Table.Schema),
		Table:  string(rowsEvent.Table.Table),
		Rows:   rows,
	}, true, nil
}

func buildApplyBatches(ctx context.Context, source *sql.DB, events []streamEvent, opts Options) ([]applyBatch, error) {
	conflictPolicy := normalizeConflictPolicy(opts.ConflictPolicy)
	if err := validateConflictPolicy(conflictPolicy); err != nil {
		return nil, err
	}

	cache := map[string]tableMetadata{}
	pending := make([]applyEvent, 0, 32)
	pendingFile := ""
	var pendingPos uint32
	batches := make([]applyBatch, 0, 16)

	for _, event := range events {
		switch event.Kind {
		case streamEventQuery:
			queryUpper := strings.ToUpper(strings.TrimSpace(event.Query))
			switch queryUpper {
			case "", "BEGIN":
				continue
			case "COMMIT":
				if len(pending) == 0 {
					continue
				}
				batches = append(batches, applyBatch{
					EndFile: event.File,
					EndPos:  event.Pos,
					Events:  pending,
				})
				pending = make([]applyEvent, 0, 32)
				pendingFile = ""
				pendingPos = 0
				continue
			case "ROLLBACK":
				pending = make([]applyEvent, 0, 32)
				pendingFile = ""
				pendingPos = 0
				continue
			}

			ddlClass, isDDL := classifyDDL(event.Query)
			if !isDDL {
				return nil, &applyFailure{
					FailureType: "unsupported_query_event",
					File:        event.File,
					Pos:         event.Pos,
					Operation:   "query",
					Query:       event.Query,
					Message:     fmt.Sprintf("unsupported query event %q at %s:%d under ROW replication", event.Query, event.File, event.Pos),
					Remediation: "run migrate --schema-only to align schema changes, then rerun replicate",
				}
			}
			switch opts.ApplyDDL {
			case "ignore":
				continue
			case "warn":
				return nil, &applyFailure{
					FailureType: "ddl_blocked",
					File:        event.File,
					Pos:         event.Pos,
					Operation:   "ddl",
					TableName:   event.Schema,
					Query:       event.Query,
					Message:     fmt.Sprintf("ddl encountered at %s:%d while --apply-ddl=warn: %s", event.File, event.Pos, event.Query),
					Remediation: "rerun with --apply-ddl=ignore and apply vetted DDL separately with migrate --schema-only",
				}
			case "apply":
				if ddlClass != ddlClassSafe {
					return nil, &applyFailure{
						FailureType: "ddl_risky_blocked",
						File:        event.File,
						Pos:         event.Pos,
						Operation:   "ddl",
						TableName:   event.Schema,
						Query:       event.Query,
						Message: fmt.Sprintf(
							"risky ddl blocked at %s:%d under --apply-ddl=apply (%s): %s",
							event.File,
							event.Pos,
							ddlClass,
							event.Query,
						),
						Remediation: "rerun with --apply-ddl=ignore and execute schema changes via migrate --schema-only after review",
					}
				}
				batches = append(batches, applyBatch{
					EndFile: event.File,
					EndPos:  event.Pos,
					Events: []applyEvent{
						{
							Query:     event.Query,
							Operation: "ddl",
							TableName: event.Schema,
						},
					},
				})
			default:
				return nil, fmt.Errorf("unsupported apply-ddl value %q", opts.ApplyDDL)
			}
		case streamEventXID:
			if len(pending) == 0 {
				continue
			}
			batches = append(batches, applyBatch{
				EndFile: event.File,
				EndPos:  event.Pos,
				Events:  pending,
			})
			pending = make([]applyEvent, 0, 32)
			pendingFile = ""
			pendingPos = 0
		case streamEventWriteRows, streamEventUpdateRows, streamEventDeleteRows:
			tableKey := tableKey(event.Schema, event.Table)
			metadata, ok := cache[tableKey]
			if !ok {
				loaded, err := loadTableMetadataFn(ctx, source, event.Schema, event.Table)
				if err != nil {
					return nil, err
				}
				cache[tableKey] = loaded
				metadata = loaded
			}

			sqlEvents, err := sqlEventsForRows(event, metadata, conflictPolicy)
			if err != nil {
				return nil, err
			}
			pending = append(pending, sqlEvents...)
			pendingFile = event.File
			pendingPos = event.Pos
		}
	}

	if len(pending) > 0 {
		return nil, &applyFailure{
			FailureType: "incomplete_transaction",
			File:        pendingFile,
			Pos:         pendingPos,
			Operation:   "transaction",
			Message:     fmt.Sprintf("incomplete transaction at %s:%d; commit not observed before window end", pendingFile, pendingPos),
			Remediation: "increase replication window or rerun replicate from previous checkpoint so full transaction can be consumed",
		}
	}
	return batches, nil
}

func sqlEventsForRows(event streamEvent, metadata tableMetadata, conflictPolicy string) ([]applyEvent, error) {
	targetName := event.Schema + "." + event.Table

	switch event.Kind {
	case streamEventWriteRows:
		out := make([]applyEvent, 0, len(event.Rows))
		for _, row := range event.Rows {
			query, args, keyArgs, err := buildInsertStatement(event.Schema, event.Table, metadata, row, conflictPolicy)
			if err != nil {
				return nil, err
			}
			out = append(out, applyEvent{
				Query:     query,
				Args:      args,
				KeyArgs:   keyArgs,
				Operation: "insert",
				TableName: targetName,
			})
		}
		return out, nil
	case streamEventDeleteRows:
		out := make([]applyEvent, 0, len(event.Rows))
		for _, row := range event.Rows {
			query, args, keyArgs, err := buildDeleteStatement(event.Schema, event.Table, metadata, row)
			if err != nil {
				return nil, err
			}
			out = append(out, applyEvent{
				Query:               query,
				Args:                args,
				KeyArgs:             keyArgs,
				Operation:           "delete",
				TableName:           targetName,
				RequireRowsAffected: conflictPolicy == "fail",
			})
		}
		return out, nil
	case streamEventUpdateRows:
		if len(event.Rows)%2 != 0 {
			return nil, fmt.Errorf("update rows event has odd row count for %s.%s", event.Schema, event.Table)
		}
		out := make([]applyEvent, 0, len(event.Rows)/2)
		for i := 0; i < len(event.Rows); i += 2 {
			oldRow := event.Rows[i]
			newRow := event.Rows[i+1]
			query, args, keyArgs, err := buildUpdateStatement(event.Schema, event.Table, metadata, oldRow, newRow)
			if err != nil {
				return nil, err
			}
			out = append(out, applyEvent{
				Query:               query,
				Args:                args,
				KeyArgs:             keyArgs,
				Operation:           "update",
				TableName:           targetName,
				RequireRowsAffected: conflictPolicy == "fail",
			})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported row event kind %s", event.Kind)
	}
}

func buildInsertStatement(schema string, table string, metadata tableMetadata, row []any, conflictPolicy string) (string, []any, []any, error) {
	if len(row) != len(metadata.Columns) {
		return "", nil, nil, fmt.Errorf("row column count mismatch for %s.%s: got=%d expected=%d (require binlog_row_image=FULL)", schema, table, len(row), len(metadata.Columns))
	}
	columnList := make([]string, 0, len(metadata.Columns))
	placeholders := make([]string, 0, len(metadata.Columns))
	for _, column := range metadata.Columns {
		quoted := quoteIdentifier(column)
		columnList = append(columnList, quoted)
		placeholders = append(placeholders, "?")
	}

	query := ""
	switch conflictPolicy {
	case "fail":
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			qualifiedTable(schema, table),
			strings.Join(columnList, ","),
			strings.Join(placeholders, ","),
		)
	case "dest-wins":
		query = fmt.Sprintf(
			"INSERT IGNORE INTO %s (%s) VALUES (%s)",
			qualifiedTable(schema, table),
			strings.Join(columnList, ","),
			strings.Join(placeholders, ","),
		)
	case "source-wins":
		updates := make([]string, 0, len(metadata.Columns))
		for _, column := range metadata.Columns {
			quoted := quoteIdentifier(column)
			updates = append(updates, fmt.Sprintf("%s=VALUES(%s)", quoted, quoted))
		}
		query = fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			qualifiedTable(schema, table),
			strings.Join(columnList, ","),
			strings.Join(placeholders, ","),
			strings.Join(updates, ","),
		)
	default:
		return "", nil, nil, fmt.Errorf("unsupported conflict policy %q", conflictPolicy)
	}

	keyArgs, err := extractKeyArgs(metadata, row)
	if err != nil {
		return "", nil, nil, err
	}

	args := make([]any, len(row))
	copy(args, row)
	return query, args, keyArgs, nil
}

func buildDeleteStatement(schema string, table string, metadata tableMetadata, row []any) (string, []any, []any, error) {
	if len(row) != len(metadata.Columns) {
		return "", nil, nil, fmt.Errorf("row column count mismatch for %s.%s: got=%d expected=%d (require binlog_row_image=FULL)", schema, table, len(row), len(metadata.Columns))
	}
	keyArgs, err := extractKeyArgs(metadata, row)
	if err != nil {
		return "", nil, nil, err
	}
	whereIndexes := keyOrdinals(metadata)
	clause, args := whereClauseFromRow(metadata.Columns, whereIndexes, row)
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", qualifiedTable(schema, table), clause)
	return query, args, keyArgs, nil
}

func buildUpdateStatement(schema string, table string, metadata tableMetadata, oldRow []any, newRow []any) (string, []any, []any, error) {
	if len(oldRow) != len(metadata.Columns) || len(newRow) != len(metadata.Columns) {
		return "", nil, nil, fmt.Errorf("row column count mismatch for %s.%s (require binlog_row_image=FULL)", schema, table)
	}

	setParts := make([]string, 0, len(metadata.Columns))
	args := make([]any, 0, len(metadata.Columns)*2)
	for i, column := range metadata.Columns {
		setParts = append(setParts, fmt.Sprintf("%s=?", quoteIdentifier(column)))
		args = append(args, newRow[i])
	}

	keyArgs, err := extractKeyArgs(metadata, oldRow)
	if err != nil {
		return "", nil, nil, err
	}
	whereIndexes := keyOrdinals(metadata)
	clause, whereArgs := whereClauseFromRow(metadata.Columns, whereIndexes, oldRow)
	args = append(args, whereArgs...)
	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s",
		qualifiedTable(schema, table),
		strings.Join(setParts, ","),
		clause,
	)
	return query, args, keyArgs, nil
}

func whereClauseFromRow(columns []string, indexes []int, row []any) (string, []any) {
	parts := make([]string, 0, len(indexes))
	args := make([]any, 0, len(indexes))
	for _, idx := range indexes {
		parts = append(parts, fmt.Sprintf("%s <=> ?", quoteIdentifier(columns[idx])))
		args = append(args, row[idx])
	}
	return strings.Join(parts, " AND "), args
}

func keyOrdinals(metadata tableMetadata) []int {
	if len(metadata.KeyOrdinals) > 0 {
		return metadata.KeyOrdinals
	}
	out := make([]int, 0, len(metadata.Columns))
	for i := range metadata.Columns {
		out = append(out, i)
	}
	return out
}

func extractKeyArgs(metadata tableMetadata, row []any) ([]any, error) {
	if len(row) != len(metadata.Columns) {
		return nil, fmt.Errorf("row column count mismatch while extracting key args: got=%d expected=%d", len(row), len(metadata.Columns))
	}
	keyIdx := keyOrdinals(metadata)
	args := make([]any, 0, len(keyIdx))
	for _, idx := range keyIdx {
		args = append(args, row[idx])
	}
	return args, nil
}

func tableKey(schema string, table string) string {
	return strings.ToLower(schema) + "." + strings.ToLower(table)
}

func loadTableMetadata(ctx context.Context, source *sql.DB, schema string, table string) (tableMetadata, error) {
	if source == nil {
		return tableMetadata{}, errors.New("source connection is required for table metadata")
	}

	rows, err := source.QueryContext(
		ctx,
		`SELECT COLUMN_NAME
		 FROM INFORMATION_SCHEMA.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`,
		schema,
		table,
	)
	if err != nil {
		return tableMetadata{}, fmt.Errorf("read column metadata for %s.%s: %w", schema, table, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	columns := make([]string, 0, 32)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return tableMetadata{}, fmt.Errorf("scan column metadata for %s.%s: %w", schema, table, err)
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return tableMetadata{}, fmt.Errorf("iterate column metadata for %s.%s: %w", schema, table, err)
	}
	if len(columns) == 0 {
		return tableMetadata{}, fmt.Errorf("table metadata not found for %s.%s", schema, table)
	}

	pkRows, err := source.QueryContext(
		ctx,
		`SELECT k.COLUMN_NAME
		 FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS t
		 JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE k
		   ON t.CONSTRAINT_SCHEMA = k.CONSTRAINT_SCHEMA
		  AND t.TABLE_NAME = k.TABLE_NAME
		  AND t.CONSTRAINT_NAME = k.CONSTRAINT_NAME
		 WHERE t.CONSTRAINT_SCHEMA = ? AND t.TABLE_NAME = ? AND t.CONSTRAINT_TYPE = 'PRIMARY KEY'
		 ORDER BY k.ORDINAL_POSITION`,
		schema,
		table,
	)
	if err != nil {
		return tableMetadata{}, fmt.Errorf("read primary key metadata for %s.%s: %w", schema, table, err)
	}
	defer func() {
		_ = pkRows.Close()
	}()

	indexByColumn := make(map[string]int, len(columns))
	for i, column := range columns {
		indexByColumn[strings.ToLower(column)] = i
	}

	keyOrdinals := make([]int, 0, 8)
	for pkRows.Next() {
		var column string
		if err := pkRows.Scan(&column); err != nil {
			return tableMetadata{}, fmt.Errorf("scan primary key metadata for %s.%s: %w", schema, table, err)
		}
		if idx, ok := indexByColumn[strings.ToLower(column)]; ok {
			keyOrdinals = append(keyOrdinals, idx)
		}
	}
	if err := pkRows.Err(); err != nil {
		return tableMetadata{}, fmt.Errorf("iterate primary key metadata for %s.%s: %w", schema, table, err)
	}
	sort.Ints(keyOrdinals)

	return tableMetadata{
		Columns:     columns,
		KeyOrdinals: keyOrdinals,
	}, nil
}

func quoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func qualifiedTable(schema string, table string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(table)
}

type ddlClassification string

const (
	ddlClassSafe  ddlClassification = "safe"
	ddlClassRisky ddlClassification = "risky"
)

func classifyDDL(query string) (ddlClassification, bool) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", false
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", false
	}
	first := strings.ToUpper(parts[0])
	switch first {
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "RENAME":
		// continue
	default:
		return "", false
	}

	normalized := " " + strings.ToUpper(trimmed) + " "
	switch first {
	case "DROP", "TRUNCATE", "RENAME":
		return ddlClassRisky, true
	case "CREATE":
		if strings.Contains(normalized, " OR REPLACE ") {
			return ddlClassRisky, true
		}
		return ddlClassSafe, true
	case "ALTER":
		if strings.Contains(normalized, " DROP ") ||
			strings.Contains(normalized, " MODIFY ") ||
			strings.Contains(normalized, " CHANGE ") ||
			strings.Contains(normalized, " RENAME ") {
			return ddlClassRisky, true
		}
		if strings.Contains(normalized, " ADD ") {
			return ddlClassSafe, true
		}
		return ddlClassRisky, true
	default:
		return ddlClassRisky, true
	}
}

func sourceSyncerConfig(rawDSN string) (replication.BinlogSyncerConfig, error) {
	normalized, err := db.NormalizeDSN(rawDSN)
	if err != nil {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("normalize source dsn for binlog sync: %w", err)
	}
	parsed, err := mysqlDriver.ParseDSN(normalized)
	if err != nil {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("parse source dsn for binlog sync: %w", err)
	}
	if parsed.Net != "tcp" {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("unsupported source network %q for binlog sync", parsed.Net)
	}

	host, portText, err := net.SplitHostPort(parsed.Addr)
	if err != nil {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("parse source host/port for binlog sync: %w", err)
	}
	portValue, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("parse source port for binlog sync: %w", err)
	}

	if parsed.TLSConfig != "" && parsed.TLSConfig != "false" && parsed.TLSConfig != "preferred" {
		return replication.BinlogSyncerConfig{}, fmt.Errorf("unsupported source tls mode %q for binlog sync", parsed.TLSConfig)
	}

	serverID := deriveServerID(parsed.User, parsed.Addr)
	flavor := "mysql"
	if parsedURI, err := url.Parse(rawDSN); err == nil && strings.EqualFold(parsedURI.Scheme, "mariadb") {
		flavor = "mariadb"
	}

	return replication.BinlogSyncerConfig{
		ServerID:  serverID,
		Flavor:    flavor,
		Host:      host,
		Port:      uint16(portValue),
		User:      parsed.User,
		Password:  parsed.Passwd,
		Charset:   "utf8mb4",
		ParseTime: true,
	}, nil
}

func deriveServerID(user string, addr string) uint32 {
	seed := fmt.Sprintf("%s@%s", user, addr)
	hash := uint32(2166136261)
	for i := 0; i < len(seed); i++ {
		hash ^= uint32(seed[i])
		hash *= 16777619
	}
	if hash == 0 {
		return 54001
	}
	return hash
}

func positionBefore(file string, pos uint32, targetFile string, targetPos uint32) bool {
	cmp := compareBinlogFile(file, targetFile)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	return pos < targetPos
}

func positionAfter(file string, pos uint32, targetFile string, targetPos uint32) bool {
	cmp := compareBinlogFile(file, targetFile)
	if cmp > 0 {
		return true
	}
	if cmp < 0 {
		return false
	}
	return pos > targetPos
}

func positionReachedOrPassed(file string, pos uint32, targetFile string, targetPos uint32) bool {
	cmp := compareBinlogFile(file, targetFile)
	if cmp > 0 {
		return true
	}
	if cmp < 0 {
		return false
	}
	return pos >= targetPos
}

func compareBinlogFile(left string, right string) int {
	if left == right {
		return 0
	}

	lPrefix, lNum, lOK := splitBinlogFile(left)
	rPrefix, rNum, rOK := splitBinlogFile(right)
	if lOK && rOK && lPrefix == rPrefix {
		if lNum < rNum {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func splitBinlogFile(name string) (string, uint64, bool) {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot >= len(name)-1 {
		return "", 0, false
	}
	prefix := name[:dot]
	suffix := name[dot+1:]
	number, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return prefix, number, true
}
