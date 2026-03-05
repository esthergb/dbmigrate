package binlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/state"
)

// Options controls checkpointed replication baseline behavior.
type Options struct {
	ApplyDDL       string
	ConflictPolicy string
	MaxEvents      uint64
	Resume         bool
	StartFile      string
	StartPos       uint32
	SourceDSN      string
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
	SourceEndFile  string
	SourceEndPos   uint32
	EndFile        string
	EndPos         uint32
	AppliedEvents  uint64
}

type applyWindow struct {
	StartFile string
	StartPos  uint32
	EndFile   string
	EndPos    uint32
}

type applyResult struct {
	File          string
	Pos           uint32
	AppliedEvents uint64
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
	RequireRowsAffected bool
}

type applyBatch struct {
	EndFile string
	EndPos  uint32
	Events  []applyEvent
}

type txRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	Commit() error
	Rollback() error
}

var (
	checkSourcePreflightFn = checkSourcePreflight
	queryBinlogPositionFn  = queryBinlogPosition
	loadApplyBatchesFn     = loadApplyBatchesFromSource
	beginDestinationTxFn   = beginDestinationTx
	applyWindowFn          = applyWindowTransactional
	timeNowFn              = time.Now
	saveConflictReportFn   = state.SaveReplicationConflictReport
)

// Run updates replication checkpoint based on current source binlog position.
func Run(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	opts.ConflictPolicy = normalizeConflictPolicy(opts.ConflictPolicy)
	if err := validateApplyDDL(opts.ApplyDDL); err != nil {
		return Summary{}, err
	}
	if err := validateConflictPolicy(opts.ConflictPolicy); err != nil {
		return Summary{}, err
	}
	if opts.StartPos > 0 && opts.StartPos < 4 {
		return Summary{}, errors.New("start-pos must be >= 4")
	}

	preflight, err := checkSourcePreflightFn(ctx, source)
	if err != nil {
		return Summary{}, err
	}

	checkpointFile := filepath.Join(stateDir, "replication-checkpoint.json")
	checkpoint := state.NewReplicationCheckpoint()
	if opts.Resume {
		loaded, err := state.LoadReplicationCheckpoint(checkpointFile)
		if err != nil {
			return Summary{}, err
		}
		checkpoint = loaded
	}

	startFile := opts.StartFile
	startPos := opts.StartPos
	if opts.Resume && checkpoint.BinlogFile != "" {
		startFile = checkpoint.BinlogFile
		startPos = checkpoint.BinlogPos
	}
	if startPos == 0 {
		startPos = 4
	}

	if startFile == "" {
		file, pos, err := queryBinlogPositionFn(ctx, source)
		if err != nil {
			return Summary{}, fmt.Errorf("query source start position: %w", err)
		}
		startFile = file
		startPos = pos
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
	checkpoint.ApplyDDL = opts.ApplyDDL
	checkpoint.UpdatedAt = timeNowFn().UTC()
	if err := state.SaveReplicationCheckpoint(checkpointFile, checkpoint); err != nil {
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
		SourceEndFile:  sourceEndFile,
		SourceEndPos:   sourceEndPos,
		EndFile:        applied.File,
		EndPos:         applied.Pos,
		AppliedEvents:  applied.AppliedEvents,
	}, nil
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

	lastFile := window.StartFile
	lastPos := window.StartPos
	var appliedEvents uint64

	for _, batch := range batches {
		if len(batch.Events) == 0 {
			continue
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
			}
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
					}
				}
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
			}
		}

		lastFile = batch.EndFile
		lastPos = batch.EndPos
		appliedEvents += uint64(len(batch.Events))
	}

	return applyResult{
		File:          lastFile,
		Pos:           lastPos,
		AppliedEvents: appliedEvents,
	}, nil
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

	return report
}
