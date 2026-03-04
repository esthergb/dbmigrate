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
	ApplyDDL  string
	Resume    bool
	StartFile string
	StartPos  uint32
}

// Summary reports checkpoint update results.
type Summary struct {
	CheckpointFile string
	ApplyDDL       string
	SourceLogBin   bool
	SourceFormat   string
	StartFile      string
	StartPos       uint32
	EndFile        string
	EndPos         uint32
}

// Run updates replication checkpoint based on current source binlog position.
func Run(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	if err := validateApplyDDL(opts.ApplyDDL); err != nil {
		return Summary{}, err
	}
	if opts.StartPos > 0 && opts.StartPos < 4 {
		return Summary{}, errors.New("start-pos must be >= 4")
	}

	preflight, err := checkSourcePreflight(ctx, source)
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
		file, pos, err := queryBinlogPosition(ctx, source)
		if err != nil {
			return Summary{}, fmt.Errorf("query source start position: %w", err)
		}
		startFile = file
		startPos = pos
	}

	endFile, endPos, err := queryBinlogPosition(ctx, source)
	if err != nil {
		// If source status is unavailable on second read, keep start as end fallback.
		endFile = startFile
		endPos = startPos
	}

	checkpoint.BinlogFile = endFile
	checkpoint.BinlogPos = endPos
	checkpoint.ApplyDDL = opts.ApplyDDL
	checkpoint.UpdatedAt = time.Now().UTC()
	if err := state.SaveReplicationCheckpoint(checkpointFile, checkpoint); err != nil {
		return Summary{}, err
	}

	return Summary{
		CheckpointFile: checkpointFile,
		ApplyDDL:       opts.ApplyDDL,
		SourceLogBin:   preflight.LogBinEnabled,
		SourceFormat:   preflight.BinlogFormat,
		StartFile:      startFile,
		StartPos:       startPos,
		EndFile:        endFile,
		EndPos:         endPos,
	}, nil
}

type preflightResult struct {
	LogBinEnabled bool
	BinlogFormat  string
}

func validateApplyDDL(value string) error {
	switch value {
	case "ignore", "apply", "warn":
		return nil
	default:
		return fmt.Errorf("invalid apply-ddl value %q (expected ignore, apply, or warn)", value)
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
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.log_bin, @@GLOBAL.binlog_format").Scan(&logBinRaw, &formatRaw); err != nil {
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

	if !logBinEnabled {
		return preflightResult{}, errors.New("source log_bin is disabled; enable binary logging before replicate")
	}
	if binlogFormat != "ROW" {
		return preflightResult{}, fmt.Errorf("source binlog_format=%s is unsupported; required=ROW for safe replication", binlogFormat)
	}

	return preflightResult{
		LogBinEnabled: logBinEnabled,
		BinlogFormat:  binlogFormat,
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
