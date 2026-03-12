package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/dblog"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/state"
)

// CopyOptions controls baseline data-copy behavior.
type CopyOptions struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	ChunkSize        int
	Resume           bool
	RequireEmptyDest bool
	Log              *dblog.Logger
}

// CopySummary reports copied data metrics.
type CopySummary struct {
	Databases      int
	Tables         int
	Completed      int
	Restarted      int
	RowsCopied     int64
	Batches        int
	CheckpointFile string
	WatermarkFile  string
	WatermarkPos   uint32
}

type sqlQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// DryRunValidationOptions controls DML dry-run validation in sandbox schemas.
type DryRunValidationOptions struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	ChunkSize        int
	MapDatabase      func(sourceDatabase string) string
}

// DryRunValidationSummary reports validated and failed DML statements in dry-run mode.
type DryRunValidationSummary struct {
	Validated int
	Failed    int
}

// CopyBaselineData copies table rows in batches with checkpoint/resume support.
func CopyBaselineData(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts CopyOptions) (CopySummary, error) {
	if source == nil || dest == nil {
		return CopySummary{}, errors.New("source and destination connections are required")
	}
	if opts.ChunkSize < 1 {
		return CopySummary{}, errors.New("chunk size must be >= 1")
	}

	checkpointFile := filepath.Join(stateDir, "data-baseline-checkpoint.json")
	checkpoint := state.NewDataCheckpoint()
	var err error
	if opts.Resume {
		checkpoint, err = state.LoadDataCheckpoint(checkpointFile)
		if err != nil {
			return CopySummary{}, err
		}
	}

	sourceConn, err := source.Conn(ctx)
	if err != nil {
		return CopySummary{}, fmt.Errorf("pin source connection: %w", err)
	}
	defer func() {
		_ = sourceConn.Close()
	}()

	if _, err := sourceConn.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return CopySummary{}, fmt.Errorf("set source transaction isolation: %w", err)
	}
	if _, err := sourceConn.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT"); err != nil {
		return CopySummary{}, fmt.Errorf("start consistent snapshot transaction: %w", err)
	}
	defer func() {
		_, _ = sourceConn.ExecContext(context.Background(), "ROLLBACK")
	}()

	if checkpoint.SourceWatermarkFile == "" {
		watermarkFile, watermarkPos, watermarkErr := querySourceWatermark(ctx, sourceConn)
		if watermarkErr != nil {
			checkpoint.SourceWatermarkFile = "unavailable"
			checkpoint.SourceWatermarkPos = 0
		} else {
			checkpoint.SourceWatermarkFile = watermarkFile
			checkpoint.SourceWatermarkPos = watermarkPos
		}
		if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
			return CopySummary{}, err
		}
	}

	databases, err := listDatabases(ctx, sourceConn)
	if err != nil {
		return CopySummary{}, fmt.Errorf("list source databases: %w", err)
	}
	selected := schema.SelectDatabases(databases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := CopySummary{CheckpointFile: checkpointFile}
	summary.WatermarkFile = checkpoint.SourceWatermarkFile
	summary.WatermarkPos = checkpoint.SourceWatermarkPos
	for _, databaseName := range selected {
		tableNames, err := listBaseTables(ctx, sourceConn, databaseName)
		if err != nil {
			return summary, fmt.Errorf("list source tables for %s: %w", databaseName, err)
		}
		if len(tableNames) == 0 {
			continue
		}
		summary.Databases++
		summary.Tables += len(tableNames)

		if opts.RequireEmptyDest {
			if err := ensureDestinationTablesAreEmpty(ctx, dest, databaseName, tableNames); err != nil {
				return summary, fmt.Errorf("destination non-empty check failed for %s: %w", databaseName, err)
			}
		}

		for _, tableName := range tableNames {
			tableKey := databaseName + "." + tableName
			progress := checkpoint.Tables[tableKey]
			if progress.Done {
				if opts.Log != nil {
					opts.Log.Debug("skipping completed table", "table", tableKey)
				}
				summary.Completed++
				continue
			}
			if opts.Log != nil {
				opts.Log.Debug("copying table", "table", tableKey, "chunk_size", opts.ChunkSize)
			}
			columns, err := listTableColumns(ctx, sourceConn, databaseName, tableName)
			if err != nil {
				return summary, fmt.Errorf("list columns for %s: %w", tableKey, err)
			}
			if len(columns) == 0 {
				progress.Done = true
				progress.UpdatedAt = time.Now().UTC()
				checkpoint.Tables[tableKey] = progress
				if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
					return summary, err
				}
				summary.Completed++
				continue
			}

			keyColumns, err := listStableKeyColumns(ctx, sourceConn, databaseName, tableName)
			if err != nil {
				return summary, fmt.Errorf("stable key check for %s: %w", tableKey, err)
			}
			if len(keyColumns) == 0 {
				return summary, fmt.Errorf("incompatible_for_live_baseline: %s has no primary key or non-null unique key; add a stable key before v1 baseline migration", tableKey)
			}

			cursor, err := checkpointCursorArgs(progress, keyColumns)
			if err != nil {
				return summary, fmt.Errorf("decode checkpoint cursor for %s: %w", tableKey, err)
			}
			if opts.Resume && progress.RowsCopied > 0 && len(cursor) == 0 {
				cursor, err = destinationResumeCursor(ctx, dest, databaseName, tableName, keyColumns)
				if err != nil {
					return summary, fmt.Errorf("resume cursor for %s: %w", tableKey, err)
				}
				if len(cursor) > 0 {
					if err := progress.SetCursorValues(cursor); err != nil {
						return summary, fmt.Errorf("encode checkpoint cursor for %s: %w", tableKey, err)
					}
					progress.KeyColumns = append([]string(nil), keyColumns...)
				}
			}

			insertSQL := buildInsertSQL(databaseName, tableName, columns)
			for {
				selectSQL := buildKeysetSelectSQL(databaseName, tableName, columns, keyColumns, len(cursor) > 0)
				batch, lastKey, err := fetchKeysetBatch(ctx, sourceConn, selectSQL, opts.ChunkSize, cursor, columns, keyColumns)
				if err != nil {
					return summary, fmt.Errorf("fetch batch for %s: %w", tableKey, err)
				}
				if len(batch) == 0 {
					break
				}

				if err := applyBatch(ctx, dest, insertSQL, batch); err != nil {
					return summary, fmt.Errorf("apply batch for %s: %w", tableKey, err)
				}

				progress.RowsCopied += int64(len(batch))
				progress.KeyColumns = append([]string(nil), keyColumns...)
				if err := progress.SetCursorValues(lastKey); err != nil {
					return summary, fmt.Errorf("encode checkpoint cursor for %s: %w", tableKey, err)
				}
				progress.UpdatedAt = time.Now().UTC()
				checkpoint.Tables[tableKey] = progress
				if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
					return summary, err
				}

				summary.RowsCopied += int64(len(batch))
				summary.Batches++
				cursor = lastKey

				if len(batch) < opts.ChunkSize {
					break
				}
			}

			progress.Done = true
			progress.UpdatedAt = time.Now().UTC()
			checkpoint.Tables[tableKey] = progress
			if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
				return summary, err
			}
			summary.Completed++
		}
	}

	return summary, nil
}

// ValidateBaselineDataDryRun executes inserts inside transactions and always rolls them back.
func ValidateBaselineDataDryRun(ctx context.Context, source *sql.DB, dest *sql.DB, opts DryRunValidationOptions) (DryRunValidationSummary, error) {
	if source == nil || dest == nil {
		return DryRunValidationSummary{}, errors.New("source and destination connections are required")
	}
	if opts.ChunkSize < 1 {
		return DryRunValidationSummary{}, errors.New("chunk size must be >= 1")
	}
	if opts.MapDatabase == nil {
		return DryRunValidationSummary{}, errors.New("MapDatabase is required")
	}

	databases, err := listDatabases(ctx, source)
	if err != nil {
		return DryRunValidationSummary{}, fmt.Errorf("list source databases: %w", err)
	}
	selected := schema.SelectDatabases(databases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := DryRunValidationSummary{}
	for _, sourceDatabase := range selected {
		destDatabase := strings.TrimSpace(opts.MapDatabase(sourceDatabase))
		if destDatabase == "" {
			return summary, fmt.Errorf("sandbox database mapping is empty for source %q", sourceDatabase)
		}
		tableNames, err := listBaseTables(ctx, source, sourceDatabase)
		if err != nil {
			return summary, fmt.Errorf("list source tables for %s: %w", sourceDatabase, err)
		}
		for _, tableName := range tableNames {
			columns, err := listTableColumns(ctx, source, sourceDatabase, tableName)
			if err != nil {
				return summary, fmt.Errorf("list columns for %s.%s: %w", sourceDatabase, tableName, err)
			}
			if len(columns) == 0 {
				continue
			}
			keyColumns, err := listStableKeyColumns(ctx, source, sourceDatabase, tableName)
			if err != nil {
				return summary, fmt.Errorf("stable key check for %s.%s: %w", sourceDatabase, tableName, err)
			}
			if len(keyColumns) == 0 {
				return summary, fmt.Errorf("incompatible_for_v1_dry_run_validation: %s.%s has no primary key or non-null unique key; add a stable key before sandbox DML validation", sourceDatabase, tableName)
			}
			selectSQL := buildKeysetSelectSQL(sourceDatabase, tableName, columns, keyColumns, false)
			insertSQL := buildInsertSQL(destDatabase, tableName, columns)
			if err := validateTableBatchWithRollback(ctx, source, dest, selectSQL, insertSQL, opts.ChunkSize, columns, keyColumns, &summary); err != nil {
				return summary, err
			}
		}
	}

	return summary, nil
}

func listDatabases(ctx context.Context, db sqlQueryer) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func listBaseTables(ctx context.Context, db sqlQueryer, databaseName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		out = append(out, tableName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return orderTableNamesByForeignKeys(ctx, db, databaseName, out)
}

func orderTableNamesByForeignKeys(ctx context.Context, db sqlQueryer, databaseName string, tableNames []string) ([]string, error) {
	if len(tableNames) < 2 {
		return tableNames, nil
	}

	dependencies := make(map[string]map[string]struct{}, len(tableNames))
	tableSet := make(map[string]struct{}, len(tableNames))
	for _, tableName := range tableNames {
		dependencies[tableName] = map[string]struct{}{}
		tableSet[tableName] = struct{}{}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_NAME, REFERENCED_TABLE_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ?
		  AND REFERENCED_TABLE_NAME IS NOT NULL
		  AND (REFERENCED_TABLE_SCHEMA IS NULL OR REFERENCED_TABLE_SCHEMA = TABLE_SCHEMA)
		ORDER BY TABLE_NAME, REFERENCED_TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var tableName string
		var referencedTableName string
		if err := rows.Scan(&tableName, &referencedTableName); err != nil {
			return nil, err
		}

		if tableName == referencedTableName {
			continue
		}
		if _, ok := tableSet[tableName]; !ok {
			continue
		}
		if _, ok := tableSet[referencedTableName]; !ok {
			continue
		}
		dependencies[tableName][referencedTableName] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	ordered, cyclic := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	if len(cyclic) > 0 {
		return nil, fmt.Errorf(
			"incompatible_for_v1_foreign_key_cycle: %s contains cyclic foreign keys across tables %s; baseline copy requires a manual post-step or temporarily relaxed FK enforcement",
			databaseName,
			strings.Join(cyclic, ", "),
		)
	}
	return ordered, nil
}

func sortTableNamesByDependencies(tableNames []string, dependencies map[string]map[string]struct{}) []string {
	ordered, _ := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	return ordered
}

func sortTableNamesByDependenciesDetailed(tableNames []string, dependencies map[string]map[string]struct{}) ([]string, []string) {
	remainingDependencies := make(map[string]map[string]struct{}, len(tableNames))
	dependents := make(map[string][]string, len(tableNames))
	for _, tableName := range tableNames {
		remainingDependencies[tableName] = map[string]struct{}{}
		for dep := range dependencies[tableName] {
			remainingDependencies[tableName][dep] = struct{}{}
			dependents[dep] = append(dependents[dep], tableName)
		}
	}
	for tableName := range dependents {
		sort.Strings(dependents[tableName])
	}

	ready := make([]string, 0, len(tableNames))
	for _, tableName := range tableNames {
		if len(remainingDependencies[tableName]) == 0 {
			ready = append(ready, tableName)
		}
	}
	sort.Strings(ready)

	ordered := make([]string, 0, len(tableNames))
	processed := make(map[string]struct{}, len(tableNames))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		if _, seen := processed[current]; seen {
			continue
		}
		processed[current] = struct{}{}
		ordered = append(ordered, current)

		for _, dependentTable := range dependents[current] {
			delete(remainingDependencies[dependentTable], current)
			if len(remainingDependencies[dependentTable]) == 0 {
				ready = append(ready, dependentTable)
			}
		}
		sort.Strings(ready)
	}

	if len(ordered) == len(tableNames) {
		return ordered, nil
	}

	remaining := make([]string, 0, len(tableNames)-len(ordered))
	for _, tableName := range tableNames {
		if _, seen := processed[tableName]; !seen {
			remaining = append(remaining, tableName)
		}
	}
	sort.Strings(remaining)
	return append(ordered, remaining...), remaining
}

func listTableColumns(ctx context.Context, db sqlQueryer, databaseName string, tableName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns := []string{}
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func buildSelectSQL(databaseName string, tableName string, columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, col := range columns {
		parts = append(parts, quoteIdentifier(col))
	}
	return fmt.Sprintf(
		"SELECT %s FROM %s.%s LIMIT ? OFFSET ?",
		strings.Join(parts, ", "),
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
	)
}

func buildKeysetSelectSQL(databaseName string, tableName string, columns []string, keyColumns []string, withCursor bool) string {
	selectColumns := make([]string, 0, len(columns))
	for _, col := range columns {
		selectColumns = append(selectColumns, quoteIdentifier(col))
	}
	orderColumns := make([]string, 0, len(keyColumns))
	for _, col := range keyColumns {
		orderColumns = append(orderColumns, quoteIdentifier(col))
	}
	base := fmt.Sprintf(
		"SELECT %s FROM %s.%s",
		strings.Join(selectColumns, ", "),
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
	)
	if withCursor {
		return fmt.Sprintf(
			"%s WHERE (%s) > (%s) ORDER BY %s LIMIT ?",
			base,
			strings.Join(orderColumns, ", "),
			strings.Join(repeat("?", len(keyColumns)), ", "),
			strings.Join(orderColumns, ", "),
		)
	}
	return fmt.Sprintf("%s ORDER BY %s LIMIT ?", base, strings.Join(orderColumns, ", "))
}

func buildInsertSQL(databaseName string, tableName string, columns []string) string {
	colParts := make([]string, 0, len(columns))
	placeholders := make([]string, 0, len(columns))
	for _, col := range columns {
		colParts = append(colParts, quoteIdentifier(col))
		placeholders = append(placeholders, "?")
	}
	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s) VALUES (%s)",
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
		strings.Join(colParts, ", "),
		strings.Join(placeholders, ", "),
	)
}

func fetchKeysetBatch(
	ctx context.Context,
	queryer sqlQueryer,
	selectSQL string,
	chunkSize int,
	cursor []any,
	columns []string,
	keyColumns []string,
) ([][]any, []any, error) {
	keyIndexes := make([]int, 0, len(keyColumns))
	for _, keyColumn := range keyColumns {
		keyIndex := -1
		for i, column := range columns {
			if column == keyColumn {
				keyIndex = i
				break
			}
		}
		if keyIndex < 0 {
			return nil, nil, fmt.Errorf("key column %q is not present in selected column list", keyColumn)
		}
		keyIndexes = append(keyIndexes, keyIndex)
	}

	args := make([]any, 0, len(cursor)+1)
	args = append(args, cursor...)
	args = append(args, chunkSize)
	rows, err := queryer.QueryContext(ctx, selectSQL, args...)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	batch := make([][]any, 0, chunkSize)
	lastKey := make([]any, 0, len(keyColumns))
	for rows.Next() {
		rowValues := make([]any, len(columns))
		scanValues := make([]any, len(columns))
		for i := range rowValues {
			scanValues[i] = &rowValues[i]
		}
		if err := rows.Scan(scanValues...); err != nil {
			return nil, nil, err
		}

		for i, val := range rowValues {
			if raw, ok := val.([]byte); ok {
				copied := make([]byte, len(raw))
				copy(copied, raw)
				rowValues[i] = copied
			}
		}
		batch = append(batch, rowValues)

		keyValues := make([]any, 0, len(keyIndexes))
		for _, keyIndex := range keyIndexes {
			keyValues = append(keyValues, rowValues[keyIndex])
		}
		lastKey = keyValues
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return batch, lastKey, nil
}

func applyBatch(ctx context.Context, db *sql.DB, insertSQL string, batch [][]any) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for _, row := range batch {
		if _, err := tx.ExecContext(ctx, insertSQL, row...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}

func validateTableBatchWithRollback(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	selectSQL string,
	insertSQL string,
	chunkSize int,
	columns []string,
	keyColumns []string,
	summary *DryRunValidationSummary,
) error {
	cursor := make([]any, 0, len(keyColumns))
	for {
		batch, lastKey, err := fetchKeysetBatch(ctx, source, selectSQL, chunkSize, cursor, columns, keyColumns)
		if err != nil {
			summary.Failed++
			return err
		}
		if len(batch) == 0 {
			break
		}

		tx, err := dest.BeginTx(ctx, nil)
		if err != nil {
			summary.Failed++
			return err
		}
		for _, row := range batch {
			if _, err := tx.ExecContext(ctx, insertSQL, row...); err != nil {
				_ = tx.Rollback()
				summary.Failed++
				return err
			}
			summary.Validated++
		}
		if err := tx.Rollback(); err != nil {
			summary.Failed++
			return err
		}
		cursor = lastKey
		if len(batch) < chunkSize {
			break
		}
	}
	return nil
}

func ensureDestinationTablesAreEmpty(ctx context.Context, db *sql.DB, databaseName string, tableNames []string) error {
	sorted := append([]string(nil), tableNames...)
	sort.Strings(sorted)

	for _, tableName := range sorted {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
		var count int64
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("table %s.%s already contains %d rows", databaseName, tableName, count)
		}
	}
	return nil
}

func listStableKeyColumns(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY
			CASE WHEN INDEX_NAME = 'PRIMARY' THEN 0 ELSE 1 END,
			NON_UNIQUE,
			INDEX_NAME,
			SEQ_IN_INDEX
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	indexes := map[string][]string{}
	nonUnique := map[string]int{}
	orderedIndexes := make([]string, 0, 4)
	for rows.Next() {
		var indexName string
		var uniqueFlag int
		var columnName string
		var seq int
		if err := rows.Scan(&indexName, &uniqueFlag, &columnName, &seq); err != nil {
			return nil, err
		}
		if _, ok := indexes[indexName]; !ok {
			orderedIndexes = append(orderedIndexes, indexName)
		}
		indexes[indexName] = append(indexes[indexName], columnName)
		nonUnique[indexName] = uniqueFlag
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	notNullColumns, err := listNotNullColumns(ctx, queryer, databaseName, tableName)
	if err != nil {
		return nil, err
	}

	for _, indexName := range orderedIndexes {
		if nonUnique[indexName] != 0 {
			continue
		}
		columns := indexes[indexName]
		if len(columns) == 0 {
			continue
		}
		eligible := true
		for _, col := range columns {
			if _, ok := notNullColumns[col]; !ok {
				eligible = false
				break
			}
		}
		if !eligible {
			continue
		}
		return columns, nil
	}

	return nil, nil
}

func listNotNullColumns(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) (map[string]struct{}, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND IS_NULLABLE = 'NO'
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func checkpointCursorArgs(progress state.TableCheckpoint, keyColumns []string) ([]any, error) {
	cursor, err := progress.CursorValues()
	if err != nil {
		return nil, err
	}
	if len(cursor) == 0 {
		return nil, nil
	}
	if len(progress.KeyColumns) == len(keyColumns) {
		for i := range keyColumns {
			if progress.KeyColumns[i] != keyColumns[i] {
				return nil, nil
			}
		}
	}
	if len(cursor) != len(keyColumns) {
		return nil, nil
	}
	return cursor, nil
}

func destinationResumeCursor(ctx context.Context, dest *sql.DB, databaseName string, tableName string, keyColumns []string) ([]any, error) {
	orderColumns := make([]string, 0, len(keyColumns))
	for _, keyColumn := range keyColumns {
		orderColumns = append(orderColumns, quoteIdentifier(keyColumn))
	}
	query := fmt.Sprintf(
		"SELECT %s FROM %s.%s ORDER BY %s LIMIT 1",
		strings.Join(orderColumns, ", "),
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
		strings.Join(orderColumns, " DESC, ")+" DESC",
	)
	rows, err := dest.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	values := make([]any, len(keyColumns))
	scan := make([]any, len(keyColumns))
	for i := range values {
		scan[i] = &values[i]
	}
	if err := rows.Scan(scan...); err != nil {
		return nil, err
	}
	return values, nil
}

func querySourceWatermark(ctx context.Context, queryer sqlQueryer) (string, uint32, error) {
	queries := []string{"SHOW MASTER STATUS", "SHOW BINARY LOG STATUS"}
	for _, query := range queries {
		file, pos, err := querySourceWatermarkWithSQL(ctx, queryer, query)
		if err == nil {
			return file, pos, nil
		}
	}
	return "", 0, errors.New("unable to query source binlog watermark")
}

func querySourceWatermarkWithSQL(ctx context.Context, queryer sqlQueryer, query string) (string, uint32, error) {
	rows, err := queryer.QueryContext(ctx, query)
	if err != nil {
		return "", 0, err
	}
	defer func() {
		_ = rows.Close()
	}()
	columns, err := rows.Columns()
	if err != nil {
		return "", 0, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", 0, err
		}
		return "", 0, errors.New("empty binlog status result")
	}
	values := make([]any, len(columns))
	scan := make([]any, len(columns))
	for i := range values {
		scan[i] = &values[i]
	}
	if err := rows.Scan(scan...); err != nil {
		return "", 0, err
	}
	file := ""
	var pos uint32
	for i, rawColumn := range columns {
		column := strings.ToLower(strings.TrimSpace(rawColumn))
		switch column {
		case "file", "log_name":
			file = stringifySQLValue(values[i])
		case "position", "pos":
			value, parseErr := parseUint32Value(values[i])
			if parseErr != nil {
				return "", 0, parseErr
			}
			pos = value
		}
	}
	if strings.TrimSpace(file) == "" || pos == 0 {
		return "", 0, errors.New("binlog watermark columns not found")
	}
	return file, pos, nil
}

func stringifySQLValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case []byte:
		return string(typed)
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func parseUint32Value(value any) (uint32, error) {
	switch typed := value.(type) {
	case int64:
		return uint32(typed), nil
	case uint64:
		return uint32(typed), nil
	case int:
		return uint32(typed), nil
	case uint:
		return uint32(typed), nil
	case []byte:
		parsed, err := strconv.ParseUint(string(typed), 10, 32)
		if err != nil {
			return 0, err
		}
		return uint32(parsed), nil
	case string:
		parsed, err := strconv.ParseUint(typed, 10, 32)
		if err != nil {
			return 0, err
		}
		return uint32(parsed), nil
	default:
		return 0, fmt.Errorf("unsupported numeric type %T", value)
	}
}

func repeat(value string, n int) []string {
	items := make([]string, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, value)
	}
	return items
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
