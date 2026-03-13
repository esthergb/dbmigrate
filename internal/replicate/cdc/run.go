package cdc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/dblog"
	"github.com/esthergb/dbmigrate/internal/throttle"
)

// Options controls CDC replication behavior.
type Options struct {
	ApplyDDL         string
	ConflictPolicy   string
	ConflictValues   string
	MaxEvents        uint64
	IncludeDatabases []string
	ExcludeDatabases []string
	Resume           bool
	BatchSize        int
	RateLimit        int
	Log              *dblog.Logger
}

// Summary reports CDC apply results.
type Summary struct {
	CheckpointFile string
	AppliedEvents  uint64
	LastCDCID      uint64
	ConflictPolicy string
}

// CDCCheckpoint stores CDC replication progress per database.
type CDCCheckpoint struct {
	Version   int               `json:"version"`
	Databases map[string]uint64 `json:"databases"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func newCDCCheckpoint() CDCCheckpoint {
	return CDCCheckpoint{Version: 1, Databases: map[string]uint64{}}
}

func loadCDCCheckpoint(path string) (CDCCheckpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newCDCCheckpoint(), nil
		}
		return CDCCheckpoint{}, fmt.Errorf("read CDC checkpoint: %w", err)
	}
	var cp CDCCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return CDCCheckpoint{}, fmt.Errorf("parse CDC checkpoint: %w", err)
	}
	if cp.Databases == nil {
		cp.Databases = map[string]uint64{}
	}
	return cp, nil
}

func saveCDCCheckpoint(path string, cp CDCCheckpoint) error {
	cp.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal CDC checkpoint: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write CDC checkpoint: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit CDC checkpoint: %w", err)
	}
	return nil
}

const defaultCDCBatchSize = 1000

var (
	readCDCEventsFn  = ReadCDCEvents
	purgeCDCEventsFn = PurgeCDCEvents
	listDatabasesFn  = listDatabases
	applyEventFn     = applyEvent
)

func listDatabases(ctx context.Context, source *sql.DB, include []string, exclude []string) ([]string, error) {
	rows, err := source.QueryContext(ctx,
		`SELECT SCHEMA_NAME FROM INFORMATION_SCHEMA.SCHEMATA
		 WHERE SCHEMA_NAME NOT IN ('information_schema','performance_schema','sys','mysql')
		 ORDER BY SCHEMA_NAME`,
	)
	if err != nil {
		return nil, fmt.Errorf("list databases: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var all []string
	for rows.Next() {
		var db string
		if err := rows.Scan(&db); err != nil {
			return nil, fmt.Errorf("scan database: %w", err)
		}
		all = append(all, db)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate databases: %w", err)
	}
	return filterDatabases(all, include, exclude), nil
}

func filterDatabases(all []string, include []string, exclude []string) []string {
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excludeSet[strings.ToLower(e)] = struct{}{}
	}
	includeSet := make(map[string]struct{}, len(include))
	for _, i := range include {
		includeSet[strings.ToLower(i)] = struct{}{}
	}
	out := make([]string, 0, len(all))
	for _, db := range all {
		key := strings.ToLower(db)
		if _, excluded := excludeSet[key]; excluded {
			continue
		}
		if len(includeSet) > 0 {
			if _, included := includeSet[key]; !included {
				continue
			}
		}
		out = append(out, db)
	}
	return out
}

func normalizeConflictPolicy(p string) string {
	if strings.TrimSpace(p) == "" {
		return "fail"
	}
	return p
}

// Run applies CDC events from source to destination for all configured databases.
func Run(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	opts.ConflictPolicy = normalizeConflictPolicy(opts.ConflictPolicy)
	if opts.BatchSize <= 0 {
		opts.BatchSize = defaultCDCBatchSize
	}

	checkpointFile := filepath.Join(stateDir, "cdc-checkpoint.json")
	checkpoint := newCDCCheckpoint()
	if opts.Resume {
		loaded, err := loadCDCCheckpoint(checkpointFile)
		if err != nil {
			return Summary{}, err
		}
		checkpoint = loaded
	}

	databases, err := listDatabasesFn(ctx, source, opts.IncludeDatabases, opts.ExcludeDatabases)
	if err != nil {
		return Summary{}, err
	}

	limiter := throttle.New(opts.RateLimit)

	var totalApplied uint64
	var lastCDCID uint64

	for _, schema := range databases {
		fromID := checkpoint.Databases[schema]

		events, err := readCDCEventsFn(ctx, source, schema, fromID, opts.BatchSize)
		if err != nil {
			return Summary{}, fmt.Errorf("read CDC events for %s: %w", schema, err)
		}
		if len(events) == 0 {
			continue
		}

		if opts.MaxEvents > 0 && totalApplied+uint64(len(events)) > opts.MaxEvents {
			remaining := opts.MaxEvents - totalApplied
			if remaining == 0 {
				break
			}
			events = events[:remaining]
		}

		for _, event := range events {
			if limiter != nil {
				limiter.Wait(1)
			}

			if err := applyEventFn(ctx, dest, schema, event, opts); err != nil {
				return Summary{}, fmt.Errorf("apply CDC event %d for %s.%s: %w", event.CDCID, schema, event.TableName, err)
			}

			checkpoint.Databases[schema] = event.CDCID
			if event.CDCID > lastCDCID {
				lastCDCID = event.CDCID
			}
			totalApplied++
		}

		if err := purgeCDCEventsFn(ctx, source, schema, checkpoint.Databases[schema]); err != nil {
			if opts.Log != nil {
				opts.Log.Debug("CDC purge warning", "schema", schema, "error", err.Error())
			}
		}

		if err := saveCDCCheckpoint(checkpointFile, checkpoint); err != nil {
			return Summary{}, err
		}
	}

	return Summary{
		CheckpointFile: checkpointFile,
		AppliedEvents:  totalApplied,
		LastCDCID:      lastCDCID,
		ConflictPolicy: opts.ConflictPolicy,
	}, nil
}

func applyEvent(ctx context.Context, dest *sql.DB, schema string, event CDCEvent, opts Options) error {
	cols, err := getDestColumnsFn(ctx, dest, schema, event.TableName)
	if err != nil {
		return err
	}

	switch event.Operation {
	case "INSERT":
		return applyInsert(ctx, dest, schema, event, cols, opts.ConflictPolicy)
	case "UPDATE":
		return applyUpdate(ctx, dest, schema, event, cols)
	case "DELETE":
		return applyDelete(ctx, dest, schema, event, cols)
	default:
		return fmt.Errorf("unknown CDC operation %q", event.Operation)
	}
}

var getDestColumnsFn = getDestColumns

func getDestColumns(ctx context.Context, dest *sql.DB, schema string, table string) ([]string, error) {
	return listTableColumnsFn(ctx, dest, schema, table)
}

func applyInsert(ctx context.Context, dest *sql.DB, schema string, event CDCEvent, columns []string, conflictPolicy string) error {
	row, err := ParseJSONRow(event.NewRowJSON)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("CDC INSERT event %d has no new_row_json", event.CDCID)
	}
	vals := RowToValues(row, columns)

	colList := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, c := range columns {
		colList[i] = quoteIdentifier(c)
		placeholders[i] = "?"
	}
	target := quoteIdentifier(schema) + "." + quoteIdentifier(event.TableName)

	var query string
	switch conflictPolicy {
	case "dest-wins":
		query = fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES (%s)",
			target, strings.Join(colList, ","), strings.Join(placeholders, ","))
	case "source-wins":
		updates := make([]string, len(columns))
		for i, c := range columns {
			updates[i] = fmt.Sprintf("%s=VALUES(%s)", quoteIdentifier(c), quoteIdentifier(c))
		}
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) ON DUPLICATE KEY UPDATE %s",
			target, strings.Join(colList, ","), strings.Join(placeholders, ","), strings.Join(updates, ","))
	default:
		query = fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			target, strings.Join(colList, ","), strings.Join(placeholders, ","))
	}
	_, err = dest.ExecContext(ctx, query, vals...)
	return err
}

func applyUpdate(ctx context.Context, dest *sql.DB, schema string, event CDCEvent, columns []string) error {
	newRow, err := ParseJSONRow(event.NewRowJSON)
	if err != nil {
		return err
	}
	if newRow == nil {
		return fmt.Errorf("CDC UPDATE event %d has no new_row_json", event.CDCID)
	}
	oldRow, err := ParseJSONRow(event.OldRowJSON)
	if err != nil {
		return err
	}

	newVals := RowToValues(newRow, columns)
	setParts := make([]string, len(columns))
	for i, c := range columns {
		setParts[i] = quoteIdentifier(c) + "=?"
	}

	target := quoteIdentifier(schema) + "." + quoteIdentifier(event.TableName)
	var whereClause string
	var whereArgs []any
	if oldRow != nil {
		whereClause, whereArgs = buildWhereFromRow(oldRow, columns)
	} else {
		whereClause, whereArgs = buildWhereFromRow(newRow, columns)
	}

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
		target, strings.Join(setParts, ","), whereClause)
	args := append(newVals, whereArgs...)
	_, err = dest.ExecContext(ctx, query, args...)
	return err
}

func applyDelete(ctx context.Context, dest *sql.DB, schema string, event CDCEvent, columns []string) error {
	row, err := ParseJSONRow(event.OldRowJSON)
	if err != nil {
		return err
	}
	if row == nil {
		return fmt.Errorf("CDC DELETE event %d has no old_row_json", event.CDCID)
	}

	target := quoteIdentifier(schema) + "." + quoteIdentifier(event.TableName)
	whereClause, whereArgs := buildWhereFromRow(row, columns)
	query := fmt.Sprintf("DELETE FROM %s WHERE %s", target, whereClause)
	_, err = dest.ExecContext(ctx, query, whereArgs...)
	return err
}

func buildWhereFromRow(row map[string]any, columns []string) (string, []any) {
	parts := make([]string, 0, len(columns))
	args := make([]any, 0, len(columns))
	for _, c := range columns {
		val := row[c]
		if val == nil {
			parts = append(parts, quoteIdentifier(c)+" IS NULL")
		} else {
			parts = append(parts, quoteIdentifier(c)+" <=> ?")
			args = append(args, val)
		}
	}
	if len(parts) == 0 {
		return "1=1", nil
	}
	return strings.Join(parts, " AND "), args
}

// VerifyTriggersExist checks that all CDC triggers are present for the given tables.
func VerifyTriggersExist(ctx context.Context, source *sql.DB, schema string, tables []string) error {
	if source == nil {
		return errors.New("verify CDC triggers: source connection is required")
	}
	for _, table := range tables {
		for _, prefix := range []string{"ins", "upd", "del"} {
			name := triggerName(prefix, table)
			var count int
			err := source.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM INFORMATION_SCHEMA.TRIGGERS
				 WHERE TRIGGER_SCHEMA = ? AND TRIGGER_NAME = ?`,
				schema, name,
			).Scan(&count)
			if err != nil {
				return fmt.Errorf("check CDC trigger %s: %w", name, err)
			}
			if count == 0 {
				return fmt.Errorf("CDC trigger %s.%s not found; run with --enable-trigger-cdc to create it", schema, name)
			}
		}
	}
	return nil
}
