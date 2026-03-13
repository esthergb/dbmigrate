package binlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/dblog"
	"github.com/esthergb/dbmigrate/internal/state"
	"github.com/esthergb/dbmigrate/internal/throttle"
)

// Options controls checkpointed replication baseline behavior.
type Options struct {
	ApplyDDL       string
	ConflictPolicy string
	ConflictValues string
	MaxEvents      uint64
	MaxLagSeconds  uint64
	SourceServerID uint32
	Idempotent     bool
	Resume         bool
	StartFile      string
	StartPos       uint32
	GTIDSet        string
	SourceDSN      string
	SourceTLSMode  string
	SourceCAFile   string
	SourceCertFile string
	SourceKeyFile  string
	RateLimit      int
	Log            *dblog.Logger
}

// Summary reports checkpoint update results.
type Summary struct {
	CheckpointFile string
	ApplyDDL       string
	ConflictPolicy string
	SourceLogBin   bool
	SourceFormat   string
	SourceRowImage string
	StartFile      string
	StartPos       uint32
	GTIDSet        string
	SourceEndFile  string
	SourceEndPos   uint32
	EndFile        string
	EndPos         uint32
	AppliedEvents  uint64
	Shape          state.ReplicationTransactionShape
}

type applyWindow struct {
	StartFile string
	StartPos  uint32
	EndFile   string
	EndPos    uint32
	GTIDSet   string
}

type applyResult struct {
	File          string
	Pos           uint32
	AppliedEvents uint64
	Shape         state.ReplicationTransactionShape
}

type applyEvent struct {
	Query               string
	Args                []any
	RowColumns          []string
	OldRowArgs          []any
	NewRowArgs          []any
	KeyColumns          []string
	KeyArgs             []any
	Operation           string
	TableName           string
	UsesFallbackKey     bool
	HasForeignKeys      bool
	RequireRowsAffected bool
}

type applyBatch struct {
	EndFile      string
	EndPos       uint32
	EndTimestamp uint32
	Events       []applyEvent
}

type txRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

var (
	checkSourcePreflightFn             = checkSourcePreflight
	queryBinlogPositionFn              = queryBinlogPosition
	querySourceGTIDSetFn               = querySourceGTIDSet
	checkGTIDEnabledFn                 = checkGTIDEnabled
	loadApplyBatchesFn                 = loadApplyBatchesFromSource
	beginDestinationTxFn               = beginDestinationTx
	applyWindowFn                      = applyWindowTransactional
	timeNowFn                          = time.Now
	saveConflictReportFn               = state.SaveReplicationConflictReport
	removeConflictReportFn             = removeConflictReport
	ensureDestinationCheckpointTableFn = ensureDestinationCheckpointTable
	saveDestinationCheckpointTxFn      = saveDestinationCheckpointTx
	loadDestinationCheckpointFn        = loadDestinationCheckpoint
)

const destinationCheckpointTable = "dbmigrate_replication_checkpoint"

// Run updates replication checkpoint based on current source binlog position.
func Run(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	opts.ConflictPolicy = normalizeConflictPolicy(opts.ConflictPolicy)
	if strings.TrimSpace(opts.ConflictValues) == "" {
		opts.ConflictValues = "redacted"
	}
	if err := validateApplyDDL(opts.ApplyDDL); err != nil {
		return Summary{}, err
	}
	if err := validateConflictPolicy(opts.ConflictPolicy); err != nil {
		return Summary{}, err
	}
	if opts.StartPos > 0 && opts.StartPos < 4 {
		return Summary{}, errors.New("start-pos must be >= 4")
	}

	gtidMode := strings.TrimSpace(opts.GTIDSet) != ""

	preflight, err := checkSourcePreflightFn(ctx, source)
	if err != nil {
		return Summary{}, err
	}

	if gtidMode {
		if err := checkGTIDEnabledFn(ctx, source); err != nil {
			return Summary{}, err
		}
	}

	checkpointFile := filepath.Join(stateDir, "replication-checkpoint.json")
	checkpoint := state.NewReplicationCheckpoint()
	if opts.Resume {
		loaded, err := state.LoadReplicationCheckpoint(checkpointFile)
		if err != nil {
			return Summary{}, err
		}
		checkpoint = loaded
		dbCheckpoint, found, err := loadDestinationCheckpointFn(ctx, dest)
		if err != nil {
			return Summary{}, fmt.Errorf("load destination replication checkpoint: %w", err)
		}
		if found {
			checkpoint.BinlogFile = dbCheckpoint.File
			checkpoint.BinlogPos = dbCheckpoint.Pos
			if strings.TrimSpace(dbCheckpoint.ApplyDDL) != "" {
				checkpoint.ApplyDDL = dbCheckpoint.ApplyDDL
			}
		}
	}

	gtidSet := opts.GTIDSet
	if opts.Resume && checkpoint.GTIDSet != "" && gtidMode {
		gtidSet = checkpoint.GTIDSet
	}

	startFile := opts.StartFile
	startPos := opts.StartPos
	if opts.Resume && checkpoint.BinlogFile != "" && !gtidMode {
		startFile = checkpoint.BinlogFile
		startPos = checkpoint.BinlogPos
	}
	if startPos == 0 && !gtidMode {
		startPos = 4
	}

	if opts.Log != nil {
		opts.Log.Debug("replication setup", "resume", opts.Resume, "gtid_mode", gtidMode, "gtid_set", gtidSet, "start_file", startFile, "start_pos", startPos, "apply_ddl", opts.ApplyDDL, "conflict_policy", opts.ConflictPolicy)
	}

	if !gtidMode && startFile == "" {
		file, pos, err := queryBinlogPositionFn(ctx, source)
		if err != nil {
			return Summary{}, fmt.Errorf("query source start position: %w", err)
		}
		startFile = file
		startPos = pos
	}

	if gtidMode && strings.TrimSpace(gtidSet) == "" {
		set, err := querySourceGTIDSetFn(ctx, source)
		if err != nil {
			return Summary{}, fmt.Errorf("query source GTID set: %w", err)
		}
		gtidSet = set
	}

	if gtidMode {
		opts.GTIDSet = gtidSet
	}

	sourceEndFile, sourceEndPos, err := queryBinlogPositionFn(ctx, source)
	if err != nil {
		// If source status is unavailable on second read, keep start as end fallback.
		sourceEndFile = startFile
		sourceEndPos = startPos
	}

	applyFailureReportPath := filepath.Join(stateDir, "replication-conflict-report.json")
	applied, err := applyWindowFn(ctx, source, dest, applyWindow{
		StartFile: startFile,
		StartPos:  startPos,
		EndFile:   sourceEndFile,
		EndPos:    sourceEndPos,
		GTIDSet:   opts.GTIDSet,
	}, opts)
	if err != nil {
		report := buildConflictReport(opts, startFile, startPos, sourceEndFile, sourceEndPos, err)
		if saveErr := saveConflictReportFn(applyFailureReportPath, report); saveErr != nil {
			return Summary{}, fmt.Errorf("replication apply failed: %v (additionally failed to write conflict report: %v)", err, saveErr)
		}
		return Summary{}, fmt.Errorf("replication apply failed: %v (conflict report: %s)", err, applyFailureReportPath)
	}

	if applied.File == "" {
		applied.File = startFile
	}
	if applied.Pos == 0 {
		applied.Pos = startPos
	}

	checkpoint.BinlogFile = applied.File
	checkpoint.BinlogPos = applied.Pos
	if gtidMode {
		endGTID, err := querySourceGTIDSetFn(ctx, source)
		if err == nil && strings.TrimSpace(endGTID) != "" {
			checkpoint.GTIDSet = endGTID
		}
	}
	checkpoint.ApplyDDL = opts.ApplyDDL
	checkpoint.Shape = applied.Shape
	checkpoint.UpdatedAt = timeNowFn().UTC()
	if err := state.SaveReplicationCheckpoint(checkpointFile, checkpoint); err != nil {
		return Summary{}, err
	}
	if err := removeConflictReportFn(applyFailureReportPath); err != nil {
		return Summary{}, err
	}

	return Summary{
		CheckpointFile: checkpointFile,
		ApplyDDL:       opts.ApplyDDL,
		ConflictPolicy: opts.ConflictPolicy,
		SourceLogBin:   preflight.LogBinEnabled,
		SourceFormat:   preflight.BinlogFormat,
		SourceRowImage: preflight.BinlogRowImage,
		StartFile:      startFile,
		StartPos:       startPos,
		GTIDSet:        opts.GTIDSet,
		SourceEndFile:  sourceEndFile,
		SourceEndPos:   sourceEndPos,
		EndFile:        applied.File,
		EndPos:         applied.Pos,
		AppliedEvents:  applied.AppliedEvents,
		Shape:          applied.Shape,
	}, nil
}

func removeConflictReport(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("clear stale replication conflict report: %w", err)
	}
	return nil
}

type preflightResult struct {
	LogBinEnabled  bool
	BinlogFormat   string
	BinlogRowImage string
}

func validateApplyDDL(value string) error {
	switch value {
	case "ignore", "apply", "warn":
		return nil
	default:
		return fmt.Errorf("invalid apply-ddl value %q (expected ignore, apply, or warn)", value)
	}
}

func normalizeConflictPolicy(value string) string {
	if strings.TrimSpace(value) == "" {
		return "fail"
	}
	return value
}

func validateConflictPolicy(value string) error {
	switch value {
	case "fail", "source-wins", "dest-wins":
		return nil
	default:
		return fmt.Errorf("invalid conflict-policy value %q (expected fail, source-wins, or dest-wins)", value)
	}
}

func queryBinlogPosition(ctx context.Context, db *sql.DB) (string, uint32, error) {
	queries := []string{"SHOW MASTER STATUS", "SHOW BINARY LOG STATUS"}
	var lastErr error
	for _, query := range queries {
		file, pos, err := queryBinlogPositionWithSQL(ctx, db, query)
		if err == nil {
			return file, pos, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return "", 0, lastErr
	}
	return "", 0, errors.New("unable to determine binlog position")
}

func querySourceGTIDSet(ctx context.Context, db *sql.DB) (string, error) {
	queries := []struct {
		sql    string
		column string
	}{
		{"SELECT @@GLOBAL.gtid_executed", "@@GLOBAL.gtid_executed"},
		{"SELECT @@GLOBAL.gtid_current_pos", "@@GLOBAL.gtid_current_pos"},
	}
	for _, q := range queries {
		var raw any
		if err := db.QueryRowContext(ctx, q.sql).Scan(&raw); err != nil {
			continue
		}
		val := toString(raw)
		if strings.TrimSpace(val) != "" {
			return val, nil
		}
	}
	return "", errors.New("unable to query source GTID set: neither gtid_executed nor gtid_current_pos is available")
}

func checkGTIDEnabled(ctx context.Context, db *sql.DB) error {
	var modeRaw any
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.gtid_mode").Scan(&modeRaw); err == nil {
		mode := strings.ToUpper(strings.TrimSpace(toString(modeRaw)))
		if mode != "ON" {
			return fmt.Errorf("source gtid_mode=%s; GTID replication requires gtid_mode=ON (MySQL)", mode)
		}
		return nil
	}
	var strictRaw any
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.gtid_strict_mode").Scan(&strictRaw); err == nil {
		return nil
	}
	return errors.New("source does not support GTID: neither gtid_mode nor gtid_strict_mode variable is available")
}

func checkSourcePreflight(ctx context.Context, db *sql.DB) (preflightResult, error) {
	var logBinRaw any
	var formatRaw any
	var rowImageRaw any
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.log_bin, @@GLOBAL.binlog_format, @@GLOBAL.binlog_row_image").Scan(&logBinRaw, &formatRaw, &rowImageRaw); err != nil {
		return preflightResult{}, fmt.Errorf("read source binlog preflight variables: %w", err)
	}

	logBinEnabled, err := parseLogBinEnabled(logBinRaw)
	if err != nil {
		return preflightResult{}, fmt.Errorf("parse source log_bin: %w", err)
	}
	binlogFormat := normalizeBinlogFormat(formatRaw)
	if binlogFormat == "" {
		return preflightResult{}, errors.New("source binlog_format is empty")
	}
	binlogRowImage := normalizeBinlogFormat(rowImageRaw)
	if binlogRowImage == "" {
		return preflightResult{}, errors.New("source binlog_row_image is empty")
	}

	if !logBinEnabled {
		return preflightResult{}, errors.New("source log_bin is disabled; enable binary logging before replicate")
	}
	if binlogFormat != "ROW" {
		return preflightResult{}, fmt.Errorf("source binlog_format=%s is unsupported; required=ROW for safe replication", binlogFormat)
	}
	if binlogRowImage != "FULL" {
		return preflightResult{}, fmt.Errorf("source binlog_row_image=%s is unsupported; required=FULL for deterministic row replay", binlogRowImage)
	}

	return preflightResult{
		LogBinEnabled:  logBinEnabled,
		BinlogFormat:   binlogFormat,
		BinlogRowImage: binlogRowImage,
	}, nil
}

func queryBinlogPositionWithSQL(ctx context.Context, db *sql.DB, query string) (string, uint32, error) {
	rows, err := db.QueryContext(ctx, query)
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
		return "", 0, errors.New("no rows returned from binlog status query")
	}

	values := make([]any, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return "", 0, err
	}

	file, pos, err := extractBinlogPosition(columns, values)
	if err != nil {
		return "", 0, err
	}
	return file, pos, nil
}

func extractBinlogPosition(columns []string, values []any) (string, uint32, error) {
	file := ""
	pos := uint32(0)

	for i, rawColumn := range columns {
		column := strings.ToLower(strings.TrimSpace(rawColumn))
		switch column {
		case "file", "log_name":
			file = toString(values[i])
		case "position", "pos":
			parsed, err := toUint32(values[i])
			if err != nil {
				return "", 0, err
			}
			pos = parsed
		}
	}

	if file == "" {
		return "", 0, errors.New("binlog file column not found in status result")
	}
	if pos == 0 {
		return "", 0, errors.New("binlog position column not found in status result")
	}
	return file, pos, nil
}

func toString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(value)
	}
}

func toUint32(value any) (uint32, error) {
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
		return 0, fmt.Errorf("unsupported position type %T", value)
	}
}

func parseLogBinEnabled(value any) (bool, error) {
	switch typed := value.(type) {
	case int64:
		return typed != 0, nil
	case uint64:
		return typed != 0, nil
	case int:
		return typed != 0, nil
	case uint:
		return typed != 0, nil
	case []byte:
		return parseLogBinEnabled(string(typed))
	case string:
		normalized := strings.ToUpper(strings.TrimSpace(typed))
		switch normalized {
		case "1", "ON", "TRUE":
			return true, nil
		case "0", "OFF", "FALSE":
			return false, nil
		default:
			return false, fmt.Errorf("unsupported log_bin value %q", typed)
		}
	default:
		return false, fmt.Errorf("unsupported log_bin type %T", value)
	}
}

func normalizeBinlogFormat(value any) string {
	switch typed := value.(type) {
	case []byte:
		return strings.ToUpper(strings.TrimSpace(string(typed)))
	case string:
		return strings.ToUpper(strings.TrimSpace(typed))
	default:
		return strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	}
}

func applyWindowTransactional(ctx context.Context, source *sql.DB, dest *sql.DB, window applyWindow, opts Options) (applyResult, error) {
	if err := ensureDestinationCheckpointTableFn(ctx, dest); err != nil {
		return applyResult{}, &applyFailure{
			FailureType: "destination_checkpoint_table",
			File:        window.StartFile,
			Pos:         window.StartPos,
			Operation:   "ensure_checkpoint_table",
			Message:     "prepare destination replication checkpoint table",
			Cause:       err,
			Remediation: "verify destination DDL permissions and rerun replicate",
			AppliedFile: window.StartFile,
			AppliedPos:  window.StartPos,
		}
	}

	batches, err := loadApplyBatchesFn(ctx, source, window, opts)
	if err != nil {
		var failure *applyFailure
		if errors.As(err, &failure) && failure != nil {
			failure.AppliedFile = window.StartFile
			failure.AppliedPos = window.StartPos
			return applyResult{}, failure
		}
		return applyResult{}, &applyFailure{
			FailureType: "load_apply_batches",
			File:        window.EndFile,
			Pos:         window.EndPos,
			Operation:   "load_batches",
			Message:     "failed to load apply batches from source binlog window",
			Cause:       err,
			Remediation: "verify source binlog permissions and connectivity, then rerun replicate",
			AppliedFile: window.StartFile,
			AppliedPos:  window.StartPos,
		}
	}

	shapeTracker := newTransactionShapeTracker(opts)
	for _, batch := range batches {
		shapeTracker.observeBatch(batch)
	}

	limiter := throttle.New(opts.RateLimit)

	lastFile := window.StartFile
	lastPos := window.StartPos
	var appliedEvents uint64

	for _, batch := range batches {
		if len(batch.Events) == 0 {
			continue
		}
		if opts.MaxLagSeconds > 0 && batch.EndTimestamp > 0 {
			lagSeconds := estimateLagSeconds(batch.EndTimestamp, timeNowFn().UTC())
			if lagSeconds > opts.MaxLagSeconds {
				return applyResult{}, &applyFailure{
					FailureType: "lag_limit_exceeded",
					File:        batch.EndFile,
					Pos:         batch.EndPos,
					Operation:   "apply_batch",
					Message: fmt.Sprintf(
						"replication lag %ds exceeds --max-lag-seconds=%d before apply",
						lagSeconds,
						opts.MaxLagSeconds,
					),
					Remediation: "increase --max-lag-seconds or rerun with a smaller apply window (for example via --max-events)",
					AppliedFile: lastFile,
					AppliedPos:  lastPos,
					Shape:       shapeTracker.snapshot(),
				}
			}
		}
		if opts.MaxEvents > 0 {
			batchEvents := uint64(len(batch.Events))
			if batchEvents > opts.MaxEvents && appliedEvents == 0 {
				return applyResult{}, &applyFailure{
					FailureType: "max_events_limit_too_low",
					File:        batch.EndFile,
					Pos:         batch.EndPos,
					Operation:   "apply_batch",
					Message: fmt.Sprintf(
						"first transaction has %d events which exceeds --max-events=%d",
						batchEvents,
						opts.MaxEvents,
					),
					Remediation: fmt.Sprintf("increase --max-events to at least %d or rerun without --max-events limit", batchEvents),
					AppliedFile: lastFile,
					AppliedPos:  lastPos,
					Shape:       shapeTracker.snapshot(),
				}
			}
			if appliedEvents+batchEvents > opts.MaxEvents {
				break
			}
		}
		if batch.EndFile == "" {
			return applyResult{}, &applyFailure{
				FailureType: "invalid_batch",
				Operation:   "apply_batch",
				Message:     "apply batch missing end file",
				Remediation: "inspect source binlog events and rerun replicate",
				AppliedFile: lastFile,
				AppliedPos:  lastPos,
				Shape:       shapeTracker.snapshot(),
			}
		}
		if batch.EndPos == 0 {
			return applyResult{}, &applyFailure{
				FailureType: "invalid_batch",
				File:        batch.EndFile,
				Operation:   "apply_batch",
				Message:     "apply batch missing end position",
				Remediation: "inspect source binlog events and rerun replicate",
				AppliedFile: lastFile,
				AppliedPos:  lastPos,
				Shape:       shapeTracker.snapshot(),
			}
		}

		if limiter != nil {
			limiter.Wait(len(batch.Events))
		}

		tx, err := beginDestinationTxFn(ctx, dest)
		if err != nil {
			return applyResult{}, &applyFailure{
				FailureType: "destination_transaction_begin",
				File:        batch.EndFile,
				Pos:         batch.EndPos,
				Operation:   "begin_tx",
				Message:     "begin destination transaction",
				Cause:       err,
				Remediation: "verify destination connectivity and transaction permissions, then rerun replicate",
				AppliedFile: lastFile,
				AppliedPos:  lastPos,
				Shape:       shapeTracker.snapshot(),
			}
		}

		for _, event := range batch.Events {
			if strings.TrimSpace(event.Query) == "" {
				_ = tx.Rollback()
				return applyResult{}, &applyFailure{
					FailureType: "invalid_apply_event",
					File:        batch.EndFile,
					Pos:         batch.EndPos,
					Operation:   event.Operation,
					TableName:   event.TableName,
					Message:     "apply event query is empty",
					Remediation: "inspect generated replication apply statements and rerun replicate",
					AppliedFile: lastFile,
					AppliedPos:  lastPos,
					Shape:       shapeTracker.snapshot(),
				}
			}
			result, err := tx.ExecContext(ctx, event.Query, event.Args...)
			if err != nil {
				_ = tx.Rollback()
				return applyResult{}, classifyApplySQLError(err, event, batch.EndFile, batch.EndPos, lastFile, lastPos)
			}
			if event.RequireRowsAffected && result != nil {
				rowsAffected, rowsErr := result.RowsAffected()
				if rowsErr == nil && rowsAffected == 0 {
					_ = tx.Rollback()
					return applyResult{}, &applyFailure{
						FailureType: "conflict_zero_rows",
						File:        batch.EndFile,
						Pos:         batch.EndPos,
						Operation:   event.Operation,
						TableName:   event.TableName,
						Query:       event.Query,
						ValueSample: buildValueSample(event.KeyColumns, event.KeyArgs),
						OldRowSample: buildValueSample(
							event.RowColumns,
							event.OldRowArgs,
						),
						NewRowSample: buildValueSample(
							event.RowColumns,
							event.NewRowArgs,
						),
						RowDiffSample: buildRowDiffSample(
							event.RowColumns,
							event.OldRowArgs,
							event.NewRowArgs,
						),
						Message: fmt.Sprintf(
							"conflict-policy=fail detected non-applied %s on %s at %s:%d",
							event.Operation,
							event.TableName,
							batch.EndFile,
							batch.EndPos,
						),
						Remediation: "review drift and rerun with --conflict-policy=source-wins or --conflict-policy=dest-wins if acceptable",
						AppliedFile: lastFile,
						AppliedPos:  lastPos,
						Shape:       shapeTracker.snapshot(),
					}
				}
			}
		}

		if err := saveDestinationCheckpointTxFn(ctx, tx, batch.EndFile, batch.EndPos, opts.ApplyDDL); err != nil {
			_ = tx.Rollback()
			return applyResult{}, &applyFailure{
				FailureType: "destination_checkpoint_write",
				File:        batch.EndFile,
				Pos:         batch.EndPos,
				Operation:   "checkpoint_write",
				Message:     "write destination replication checkpoint in transaction",
				Cause:       err,
				Remediation: "verify destination write permissions for checkpoint table and rerun replicate",
				AppliedFile: lastFile,
				AppliedPos:  lastPos,
				Shape:       shapeTracker.snapshot(),
			}
		}

		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return applyResult{}, &applyFailure{
				FailureType: "destination_transaction_commit",
				File:        batch.EndFile,
				Pos:         batch.EndPos,
				Operation:   "commit_tx",
				Message:     fmt.Sprintf("commit destination transaction at %s:%d", batch.EndFile, batch.EndPos),
				Cause:       err,
				Remediation: "verify destination transaction durability settings and rerun replicate",
				AppliedFile: lastFile,
				AppliedPos:  lastPos,
				Shape:       shapeTracker.snapshot(),
			}
		}

		lastFile = batch.EndFile
		lastPos = batch.EndPos
		appliedEvents += uint64(len(batch.Events))
		shapeTracker.markApplied(batch)
	}

	return applyResult{
		File:          lastFile,
		Pos:           lastPos,
		AppliedEvents: appliedEvents,
		Shape:         shapeTracker.snapshot(),
	}, nil
}

type destinationCheckpoint struct {
	File     string
	Pos      uint32
	ApplyDDL string
}

func ensureDestinationCheckpointTable(ctx context.Context, dest *sql.DB) error {
	ddl := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			checkpoint_id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
			binlog_file VARCHAR(255) NOT NULL,
			binlog_pos BIGINT UNSIGNED NOT NULL,
			apply_ddl VARCHAR(16) NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB
	`, destinationCheckpointTable)
	_, err := dest.ExecContext(ctx, ddl)
	return err
}

func saveDestinationCheckpointTx(ctx context.Context, tx txRunner, file string, pos uint32, applyDDL string) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (checkpoint_id, binlog_file, binlog_pos, apply_ddl)
		VALUES (1, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			binlog_file = VALUES(binlog_file),
			binlog_pos = VALUES(binlog_pos),
			apply_ddl = VALUES(apply_ddl),
			updated_at = CURRENT_TIMESTAMP
	`, destinationCheckpointTable)
	_, err := tx.ExecContext(ctx, query, file, pos, applyDDL)
	return err
}

func loadDestinationCheckpoint(ctx context.Context, dest *sql.DB) (destinationCheckpoint, bool, error) {
	if err := ensureDestinationCheckpointTable(ctx, dest); err != nil {
		return destinationCheckpoint{}, false, err
	}
	query := fmt.Sprintf("SELECT binlog_file, binlog_pos, apply_ddl FROM %s WHERE checkpoint_id = 1", destinationCheckpointTable)
	var file string
	var pos uint64
	var applyDDL string
	err := dest.QueryRowContext(ctx, query).Scan(&file, &pos, &applyDDL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return destinationCheckpoint{}, false, nil
		}
		return destinationCheckpoint{}, false, err
	}
	return destinationCheckpoint{
		File:     file,
		Pos:      uint32(pos),
		ApplyDDL: applyDDL,
	}, true, nil
}

func estimateLagSeconds(eventTimestamp uint32, now time.Time) uint64 {
	eventUnix := int64(eventTimestamp)
	nowUnix := now.Unix()
	if nowUnix <= eventUnix {
		return 0
	}
	return uint64(nowUnix - eventUnix)
}

func beginDestinationTx(ctx context.Context, db *sql.DB) (txRunner, error) {
	return db.BeginTx(ctx, nil)
}

func buildConflictReport(opts Options, startFile string, startPos uint32, sourceEndFile string, sourceEndPos uint32, err error) state.ReplicationConflictReport {
	report := state.NewReplicationConflictReport()
	report.GeneratedAt = timeNowFn().UTC()
	report.ApplyDDL = opts.ApplyDDL
	report.ConflictPolicy = normalizeConflictPolicy(opts.ConflictPolicy)
	report.StartFile = startFile
	report.StartPos = startPos
	report.SourceEndFile = sourceEndFile
	report.SourceEndPos = sourceEndPos
	report.AppliedEndFile = startFile
	report.AppliedEndPos = startPos
	report.FailureType = "replication_error"
	report.Message = err.Error()

	var failure *applyFailure
	if errors.As(err, &failure) && failure != nil {
		if failure.FailureType != "" {
			report.FailureType = failure.FailureType
		}
		report.SQLErrorCode = failure.SQLErrorCode
		report.Message = failure.Error()
		report.Operation = failure.Operation
		report.TableName = failure.TableName
		report.Query = failure.Query
		report.ValueSample = failure.ValueSample
		report.OldRowSample = failure.OldRowSample
		report.NewRowSample = failure.NewRowSample
		report.RowDiffSample = failure.RowDiffSample
		report.Shape = failure.Shape
		report.Remediation = failure.Remediation
		if failure.File != "" {
			report.SourceEndFile = failure.File
		}
		if failure.Pos > 0 {
			report.SourceEndPos = failure.Pos
		}
		if failure.AppliedFile != "" {
			report.AppliedEndFile = failure.AppliedFile
		}
		if failure.AppliedPos > 0 {
			report.AppliedEndPos = failure.AppliedPos
		}
	}

	if opts.ConflictValues != "plain" {
		report.ValuesRedacted = true
		report.ValueSample = redactSamples(report.ValueSample)
		report.OldRowSample = redactSamples(report.OldRowSample)
		report.NewRowSample = redactSamples(report.NewRowSample)
		report.RowDiffSample = redactSamples(report.RowDiffSample)
	}

	return report
}

func redactSamples(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(trimmed, "... +") && strings.HasSuffix(trimmed, "more") {
			out = append(out, trimmed)
			continue
		}
		if strings.Contains(trimmed, ":") {
			parts := strings.SplitN(trimmed, ":", 2)
			out = append(out, parts[0]+":<redacted>")
			continue
		}
		if strings.Contains(trimmed, "=") {
			parts := strings.SplitN(trimmed, "=", 2)
			out = append(out, parts[0]+"=<redacted>")
			continue
		}
		out = append(out, "<redacted>")
	}
	return out
}
