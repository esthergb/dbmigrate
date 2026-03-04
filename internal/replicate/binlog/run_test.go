package binlog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"

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

func TestValidateConflictPolicy(t *testing.T) {
	valid := []string{"fail", "source-wins", "dest-wins"}
	for _, value := range valid {
		if err := validateConflictPolicy(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}
	if err := validateConflictPolicy("merge"); err == nil {
		t.Fatal("expected invalid conflict-policy value to fail")
	}
}

func TestClassifyApplySQLErrorDuplicateKey(t *testing.T) {
	failure := classifyApplySQLError(
		&mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry"},
		applyEvent{
			Operation: "insert",
			TableName: "app.items",
			Query:     "INSERT INTO `app`.`items` (`id`) VALUES (?)",
			KeyColumns: []string{
				"id",
			},
			KeyArgs: []any{int64(42)},
		},
		"mysql-bin.000001",
		220,
		"mysql-bin.000001",
		200,
	)

	if failure.FailureType != "conflict_duplicate_key" {
		t.Fatalf("unexpected failure type: %s", failure.FailureType)
	}
	if failure.SQLErrorCode != 1062 {
		t.Fatalf("unexpected sql error code: %d", failure.SQLErrorCode)
	}
	if !strings.Contains(failure.Remediation, "--conflict-policy=source-wins") {
		t.Fatalf("unexpected remediation: %s", failure.Remediation)
	}
	if len(failure.ValueSample) != 1 || failure.ValueSample[0] != "id=42" {
		t.Fatalf("unexpected value sample: %#v", failure.ValueSample)
	}
}

func TestClassifyApplySQLErrorSchemaDrift(t *testing.T) {
	failure := classifyApplySQLError(
		&mysqlDriver.MySQLError{Number: 1054, Message: "Unknown column"},
		applyEvent{
			Operation: "update",
			TableName: "app.items",
			Query:     "UPDATE `app`.`items` SET `x`=? WHERE `id` <=> ?",
			KeyColumns: []string{
				"id",
			},
			KeyArgs: []any{int64(7)},
		},
		"mysql-bin.000001",
		320,
		"mysql-bin.000001",
		300,
	)

	if failure.FailureType != "schema_drift" {
		t.Fatalf("unexpected failure type: %s", failure.FailureType)
	}
	if !strings.Contains(failure.Remediation, "migrate --schema-only") {
		t.Fatalf("unexpected remediation: %s", failure.Remediation)
	}
}

func TestBuildValueSampleTruncatesAndLimits(t *testing.T) {
	longText := strings.Repeat("a", 200)
	sample := buildValueSample(
		[]string{"id", "description", "payload", "optional", "c5", "c6", "c7"},
		[]any{int64(1), longText, []byte("blob"), nil, "x", "y", "z"},
	)
	if len(sample) != 7 {
		t.Fatalf("unexpected sample length: %d", len(sample))
	}
	if !strings.HasPrefix(sample[2], "payload=blob") {
		t.Fatalf("unexpected blob sample: %s", sample[2])
	}
	if !strings.Contains(sample[len(sample)-1], "... +1 more") {
		t.Fatalf("expected overflow marker, got: %s", sample[len(sample)-1])
	}
}

func TestBuildValueSampleFallsBackToOrdinalLabels(t *testing.T) {
	sample := buildValueSample(nil, []any{int64(9)})
	if len(sample) != 1 || sample[0] != "v1=9" {
		t.Fatalf("unexpected sample: %#v", sample)
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

func TestRunWritesConflictReportOnApplyFailure(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	checkSourcePreflightFn = func(_ context.Context, _ *sql.DB) (preflightResult, error) {
		return preflightResult{
			LogBinEnabled:  true,
			BinlogFormat:   "ROW",
			BinlogRowImage: "FULL",
		}, nil
	}

	queryCalls := 0
	queryBinlogPositionFn = func(_ context.Context, _ *sql.DB) (string, uint32, error) {
		queryCalls++
		if queryCalls == 1 {
			return "mysql-bin.000100", 400, nil
		}
		return "mysql-bin.000100", 480, nil
	}

	applyWindowFn = func(_ context.Context, _ *sql.DB, _ *sql.DB, _ applyWindow, _ Options) (applyResult, error) {
		return applyResult{}, &applyFailure{
			FailureType: "ddl_risky_blocked",
			File:        "mysql-bin.000100",
			Pos:         460,
			Operation:   "ddl",
			TableName:   "app",
			Query:       "DROP TABLE app.items",
			Message:     "risky ddl blocked at mysql-bin.000100:460",
			Remediation: "rerun with --apply-ddl=ignore and apply DDL manually",
			AppliedFile: "mysql-bin.000100",
			AppliedPos:  420,
		}
	}

	tmp := t.TempDir()
	_, err := Run(context.Background(), &sql.DB{}, &sql.DB{}, tmp, Options{
		ApplyDDL:       "apply",
		ConflictPolicy: "fail",
		Resume:         false,
	})
	if err == nil {
		t.Fatal("expected run to fail")
	}
	if !strings.Contains(err.Error(), "replication-conflict-report.json") {
		t.Fatalf("expected error to include conflict report path, got: %v", err)
	}

	report, err := state.LoadReplicationConflictReport(filepath.Join(tmp, "replication-conflict-report.json"))
	if err != nil {
		t.Fatalf("load conflict report: %v", err)
	}
	if report.FailureType != "ddl_risky_blocked" {
		t.Fatalf("unexpected failure type: %q", report.FailureType)
	}
	if report.Operation != "ddl" {
		t.Fatalf("unexpected operation: %q", report.Operation)
	}
	if report.SQLErrorCode != 0 {
		t.Fatalf("unexpected sql error code: %d", report.SQLErrorCode)
	}
	if report.AppliedEndPos != 420 {
		t.Fatalf("unexpected applied end pos: %d", report.AppliedEndPos)
	}
	if report.SourceEndPos != 460 {
		t.Fatalf("unexpected source end pos: %d", report.SourceEndPos)
	}
}

func TestRunWritesSQLErrorCodeInConflictReport(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	checkSourcePreflightFn = func(_ context.Context, _ *sql.DB) (preflightResult, error) {
		return preflightResult{
			LogBinEnabled:  true,
			BinlogFormat:   "ROW",
			BinlogRowImage: "FULL",
		}, nil
	}
	queryBinlogPositionFn = func(_ context.Context, _ *sql.DB) (string, uint32, error) {
		return "mysql-bin.000200", 600, nil
	}
	applyWindowFn = func(_ context.Context, _ *sql.DB, _ *sql.DB, _ applyWindow, _ Options) (applyResult, error) {
		return applyResult{}, &applyFailure{
			FailureType:  "schema_drift",
			File:         "mysql-bin.000200",
			Pos:          600,
			SQLErrorCode: 1054,
			Operation:    "update",
			TableName:    "app.items",
			Query:        "UPDATE `app`.`items` SET `missing`=? WHERE `id` <=> ?",
			ValueSample:  []string{"id=42"},
			Message:      "apply event at mysql-bin.000200:600 failed",
			Remediation:  "run migrate --schema-only",
			AppliedFile:  "mysql-bin.000200",
			AppliedPos:   560,
		}
	}

	tmp := t.TempDir()
	_, err := Run(context.Background(), &sql.DB{}, &sql.DB{}, tmp, Options{
		ApplyDDL:       "warn",
		ConflictPolicy: "fail",
	})
	if err == nil {
		t.Fatal("expected run to fail")
	}

	report, err := state.LoadReplicationConflictReport(filepath.Join(tmp, "replication-conflict-report.json"))
	if err != nil {
		t.Fatalf("load conflict report: %v", err)
	}
	if report.SQLErrorCode != 1054 {
		t.Fatalf("unexpected sql error code: %d", report.SQLErrorCode)
	}
	if report.FailureType != "schema_drift" {
		t.Fatalf("unexpected failure type: %s", report.FailureType)
	}
	if len(report.ValueSample) != 1 || report.ValueSample[0] != "id=42" {
		t.Fatalf("unexpected value sample: %#v", report.ValueSample)
	}
}

func TestApplyWindowTransactionalNoBatches(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	loadApplyBatchesFn = func(_ context.Context, _ *sql.DB, _ applyWindow, _ Options) ([]applyBatch, error) {
		return nil, nil
	}
	beginDestinationTxFn = func(_ context.Context, _ *sql.DB) (txRunner, error) {
		t.Fatal("begin transaction should not be called when no batches exist")
		return nil, nil
	}

	result, err := applyWindowTransactional(context.Background(), nil, nil, applyWindow{
		StartFile: "mysql-bin.000001",
		StartPos:  4,
		EndFile:   "mysql-bin.000001",
		EndPos:    200,
	}, Options{ApplyDDL: "warn"})
	if err != nil {
		t.Fatalf("applyWindowTransactional: %v", err)
	}
	if result.File != "mysql-bin.000001" || result.Pos != 4 || result.AppliedEvents != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestApplyWindowTransactionalAdvancesOnCommit(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	loadApplyBatchesFn = func(_ context.Context, _ *sql.DB, _ applyWindow, _ Options) ([]applyBatch, error) {
		return []applyBatch{
			{
				EndFile: "mysql-bin.000011",
				EndPos:  110,
				Events:  []applyEvent{{Query: "INSERT INTO t VALUES (?)", Args: []any{1}}},
			},
			{
				EndFile: "mysql-bin.000011",
				EndPos:  160,
				Events: []applyEvent{
					{Query: "UPDATE t SET c=? WHERE id=?", Args: []any{2, 1}},
					{Query: "DELETE FROM t WHERE id=?", Args: []any{3}},
				},
			},
		}, nil
	}

	txs := []*fakeTx{{}, {}}
	beginCalls := 0
	beginDestinationTxFn = func(_ context.Context, _ *sql.DB) (txRunner, error) {
		tx := txs[beginCalls]
		beginCalls++
		return tx, nil
	}

	result, err := applyWindowTransactional(context.Background(), nil, nil, applyWindow{
		StartFile: "mysql-bin.000011",
		StartPos:  100,
		EndFile:   "mysql-bin.000011",
		EndPos:    160,
	}, Options{ApplyDDL: "warn"})
	if err != nil {
		t.Fatalf("applyWindowTransactional: %v", err)
	}
	if result.File != "mysql-bin.000011" || result.Pos != 160 || result.AppliedEvents != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if beginCalls != 2 {
		t.Fatalf("unexpected begin calls: %d", beginCalls)
	}
	if !txs[0].committed || txs[0].rolledBack {
		t.Fatalf("unexpected tx0 state: %+v", txs[0])
	}
	if !txs[1].committed || txs[1].rolledBack {
		t.Fatalf("unexpected tx1 state: %+v", txs[1])
	}
}

func TestApplyWindowTransactionalExecErrorRollsBack(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	loadApplyBatchesFn = func(_ context.Context, _ *sql.DB, _ applyWindow, _ Options) ([]applyBatch, error) {
		return []applyBatch{
			{
				EndFile: "mysql-bin.000030",
				EndPos:  150,
				Events:  []applyEvent{{Query: "INSERT INTO t VALUES (?)", Args: []any{1}}},
			},
			{
				EndFile: "mysql-bin.000030",
				EndPos:  220,
				Events:  []applyEvent{{Query: "UPDATE t SET c=? WHERE id=?", Args: []any{2, 1}}},
			},
		}, nil
	}

	txs := []*fakeTx{{}, {execErr: errors.New("boom")}}
	beginCalls := 0
	beginDestinationTxFn = func(_ context.Context, _ *sql.DB) (txRunner, error) {
		tx := txs[beginCalls]
		beginCalls++
		return tx, nil
	}

	_, err := applyWindowTransactional(context.Background(), nil, nil, applyWindow{
		StartFile: "mysql-bin.000030",
		StartPos:  120,
		EndFile:   "mysql-bin.000030",
		EndPos:    220,
	}, Options{ApplyDDL: "warn"})
	if err == nil {
		t.Fatal("expected applyWindowTransactional to fail on exec error")
	}
	if !strings.Contains(err.Error(), "apply event at mysql-bin.000030:220") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !txs[0].committed || txs[0].rolledBack {
		t.Fatalf("unexpected tx0 state: %+v", txs[0])
	}
	if txs[1].committed || !txs[1].rolledBack {
		t.Fatalf("unexpected tx1 state: %+v", txs[1])
	}
}

func TestApplyWindowTransactionalCommitErrorRollsBack(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	loadApplyBatchesFn = func(_ context.Context, _ *sql.DB, _ applyWindow, _ Options) ([]applyBatch, error) {
		return []applyBatch{
			{
				EndFile: "mysql-bin.000040",
				EndPos:  300,
				Events:  []applyEvent{{Query: "INSERT INTO t VALUES (?)", Args: []any{1}}},
			},
		}, nil
	}

	tx := &fakeTx{commitErr: errors.New("commit failed")}
	beginDestinationTxFn = func(_ context.Context, _ *sql.DB) (txRunner, error) {
		return tx, nil
	}

	_, err := applyWindowTransactional(context.Background(), nil, nil, applyWindow{
		StartFile: "mysql-bin.000040",
		StartPos:  240,
		EndFile:   "mysql-bin.000040",
		EndPos:    300,
	}, Options{ApplyDDL: "warn"})
	if err == nil {
		t.Fatal("expected applyWindowTransactional to fail on commit error")
	}
	if !strings.Contains(err.Error(), "commit destination transaction at mysql-bin.000040:300") {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("unexpected tx state: %+v", tx)
	}
}

func TestApplyWindowTransactionalFailPolicyRequiresAffectedRows(t *testing.T) {
	restore := stubRunHooks(t)
	defer restore()

	loadApplyBatchesFn = func(_ context.Context, _ *sql.DB, _ applyWindow, _ Options) ([]applyBatch, error) {
		return []applyBatch{
			{
				EndFile: "mysql-bin.000050",
				EndPos:  340,
				Events: []applyEvent{
					{
						Query:               "UPDATE `app`.`t` SET `c`=? WHERE `id` <=> ?",
						Args:                []any{3, 1},
						Operation:           "update",
						TableName:           "app.t",
						RequireRowsAffected: true,
					},
				},
			},
		}, nil
	}

	tx := &fakeTx{rowsAffected: 0, rowsSet: true}
	beginDestinationTxFn = func(_ context.Context, _ *sql.DB) (txRunner, error) {
		return tx, nil
	}

	_, err := applyWindowTransactional(context.Background(), nil, nil, applyWindow{
		StartFile: "mysql-bin.000050",
		StartPos:  300,
		EndFile:   "mysql-bin.000050",
		EndPos:    340,
	}, Options{ApplyDDL: "warn", ConflictPolicy: "fail"})
	if err == nil {
		t.Fatal("expected applyWindowTransactional to fail when rows affected is zero")
	}
	if !strings.Contains(err.Error(), "conflict-policy=fail detected non-applied update") {
		t.Fatalf("unexpected error: %v", err)
	}
	if tx.committed || !tx.rolledBack {
		t.Fatalf("unexpected tx state: %+v", tx)
	}
}

func stubRunHooks(t *testing.T) func() {
	t.Helper()

	origPreflight := checkSourcePreflightFn
	origQuery := queryBinlogPositionFn
	origLoadBatches := loadApplyBatchesFn
	origBeginTx := beginDestinationTxFn
	origApply := applyWindowFn
	origNow := timeNowFn
	origSaveReport := saveConflictReportFn

	return func() {
		checkSourcePreflightFn = origPreflight
		queryBinlogPositionFn = origQuery
		loadApplyBatchesFn = origLoadBatches
		beginDestinationTxFn = origBeginTx
		applyWindowFn = origApply
		timeNowFn = origNow
		saveConflictReportFn = origSaveReport
	}
}

type fakeTx struct {
	execErr      error
	commitErr    error
	rowsAffected int64
	rowsSet      bool
	execCalls    int
	committed    bool
	rolledBack   bool
	lastQueries  []string
}

func (f *fakeTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	f.execCalls++
	f.lastQueries = append(f.lastQueries, query)
	if f.execErr != nil {
		return nil, f.execErr
	}
	affected := f.rowsAffected
	if !f.rowsSet {
		affected = 1
	}
	return fakeSQLResult{rows: affected}, nil
}

func (f *fakeTx) Commit() error {
	if f.commitErr != nil {
		return f.commitErr
	}
	f.committed = true
	return nil
}

func (f *fakeTx) Rollback() error {
	f.rolledBack = true
	return nil
}

type fakeSQLResult struct {
	rows int64
}

func (f fakeSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (f fakeSQLResult) RowsAffected() (int64, error) {
	return f.rows, nil
}
