package binlog

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/esthergb/dbmigrate/internal/state"
)

func TestValidateApplyDDL(t *testing.T) {
	valid := []string{"ignore", "apply", "warn"}
	for _, value := range valid {
		if err := validateApplyDDL(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}

	if err := validateApplyDDL("invalid"); err == nil {
		t.Fatal("expected invalid apply-ddl value to fail")
	}
}

func TestExtractBinlogPositionMySQLColumns(t *testing.T) {
	columns := []string{"File", "Position", "Binlog_Do_DB"}
	values := []any{[]byte("mysql-bin.000123"), int64(456), nil}

	file, pos, err := extractBinlogPosition(columns, values)
	if err != nil {
		t.Fatalf("extract binlog position: %v", err)
	}
	if file != "mysql-bin.000123" {
		t.Fatalf("unexpected file: %q", file)
	}
	if pos != 456 {
		t.Fatalf("unexpected pos: %d", pos)
	}
}

func TestExtractBinlogPositionMariaColumns(t *testing.T) {
	columns := []string{"Log_name", "Pos"}
	values := []any{[]byte("mariadb-bin.000007"), []byte("8910")}

	file, pos, err := extractBinlogPosition(columns, values)
	if err != nil {
		t.Fatalf("extract binlog position: %v", err)
	}
	if file != "mariadb-bin.000007" {
		t.Fatalf("unexpected file: %q", file)
	}
	if pos != 8910 {
		t.Fatalf("unexpected pos: %d", pos)
	}
}

func TestExtractBinlogPositionMissingColumns(t *testing.T) {
	columns := []string{"a", "b"}
	values := []any{"x", "y"}
	if _, _, err := extractBinlogPosition(columns, values); err == nil {
		t.Fatal("expected missing columns to fail")
	}
}

func TestParseLogBinEnabled(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{in: int64(1), want: true},
		{in: int64(0), want: false},
		{in: "ON", want: true},
		{in: "off", want: false},
		{in: []byte("TRUE"), want: true},
		{in: []byte("FALSE"), want: false},
	}

	for _, tc := range cases {
		got, err := parseLogBinEnabled(tc.in)
		if err != nil {
			t.Fatalf("parseLogBinEnabled(%v): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseLogBinEnabled(%v)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseLogBinEnabledInvalid(t *testing.T) {
	if _, err := parseLogBinEnabled("MAYBE"); err == nil {
		t.Fatal("expected invalid log_bin value to fail")
	}
}

func TestNormalizeBinlogFormat(t *testing.T) {
	if got := normalizeBinlogFormat("row"); got != "ROW" {
		t.Fatalf("unexpected normalized format: %q", got)
	}
	if got := normalizeBinlogFormat([]byte("mixed")); got != "MIXED" {
		t.Fatalf("unexpected normalized format: %q", got)
	}
}

func TestRunNoopApplyKeepsCheckpointAtStart(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	checkSourcePreflightFn = func(_ context.Context, _ *sql.DB) (preflightResult, error) {
		return preflightResult{LogBinEnabled: true, BinlogFormat: "ROW"}, nil
	}

	queryCalls := 0
	queryBinlogPositionFn = func(_ context.Context, _ *sql.DB) (string, uint32, error) {
		queryCalls++
		if queryCalls == 1 {
			return "mysql-bin.000010", 120, nil
		}
		return "mysql-bin.000010", 220, nil
	}

	tmp := t.TempDir()
	summary, err := Run(context.Background(), &sql.DB{}, &sql.DB{}, tmp, Options{
		ApplyDDL: "warn",
		Resume:   false,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if summary.StartFile != "mysql-bin.000010" || summary.StartPos != 120 {
		t.Fatalf("unexpected start position: %s:%d", summary.StartFile, summary.StartPos)
	}
	if summary.SourceEndFile != "mysql-bin.000010" || summary.SourceEndPos != 220 {
		t.Fatalf("unexpected source end position: %s:%d", summary.SourceEndFile, summary.SourceEndPos)
	}
	if summary.EndFile != "mysql-bin.000010" || summary.EndPos != 120 {
		t.Fatalf("unexpected applied end position: %s:%d", summary.EndFile, summary.EndPos)
	}
	if summary.AppliedEvents != 0 {
		t.Fatalf("unexpected applied events: %d", summary.AppliedEvents)
	}

	cp, err := state.LoadReplicationCheckpoint(filepath.Join(tmp, "replication-checkpoint.json"))
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if cp.BinlogFile != "mysql-bin.000010" || cp.BinlogPos != 120 {
		t.Fatalf("checkpoint advanced unexpectedly: %s:%d", cp.BinlogFile, cp.BinlogPos)
	}
}

func TestRunCheckpointAdvancesOnlyToAppliedEnd(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	checkSourcePreflightFn = func(_ context.Context, _ *sql.DB) (preflightResult, error) {
		return preflightResult{LogBinEnabled: true, BinlogFormat: "ROW"}, nil
	}

	queryBinlogPositionFn = func(_ context.Context, _ *sql.DB) (string, uint32, error) {
		return "mysql-bin.000020", 500, nil
	}

	applyWindowFn = func(_ context.Context, _ *sql.DB, _ *sql.DB, window applyWindow, _ Options) (applyResult, error) {
		if window.StartFile != "mysql-bin.000020" || window.StartPos != 300 {
			t.Fatalf("unexpected apply window start: %s:%d", window.StartFile, window.StartPos)
		}
		if window.EndFile != "mysql-bin.000020" || window.EndPos != 500 {
			t.Fatalf("unexpected apply window end: %s:%d", window.EndFile, window.EndPos)
		}
		return applyResult{
			File:          "mysql-bin.000020",
			Pos:           360,
			AppliedEvents: 7,
		}, nil
	}

	tmp := t.TempDir()
	checkpointFile := filepath.Join(tmp, "replication-checkpoint.json")
	if err := state.SaveReplicationCheckpoint(checkpointFile, state.ReplicationCheckpoint{
		Version:    1,
		BinlogFile: "mysql-bin.000020",
		BinlogPos:  300,
		ApplyDDL:   "warn",
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	summary, err := Run(context.Background(), &sql.DB{}, &sql.DB{}, tmp, Options{
		ApplyDDL: "warn",
		Resume:   true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if summary.SourceEndPos != 500 || summary.EndPos != 360 || summary.AppliedEvents != 7 {
		t.Fatalf("unexpected summary positions/events: source_end=%d applied_end=%d applied_events=%d", summary.SourceEndPos, summary.EndPos, summary.AppliedEvents)
	}

	cp, err := state.LoadReplicationCheckpoint(checkpointFile)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if cp.BinlogPos != 360 {
		t.Fatalf("unexpected checkpoint position: %d", cp.BinlogPos)
	}
}

func stubRunHooks(t *testing.T) func() {
	t.Helper()

	origPreflight := checkSourcePreflightFn
	origQuery := queryBinlogPositionFn
	origApply := applyWindowFn
	origNow := timeNowFn

	return func() {
		checkSourcePreflightFn = origPreflight
		queryBinlogPositionFn = origQuery
		applyWindowFn = origApply
		timeNowFn = origNow
	}
}
