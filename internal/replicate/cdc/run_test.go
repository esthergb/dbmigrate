package cdc

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestRunNilConnections(t *testing.T) {
	_, err := Run(context.Background(), nil, nil, t.TempDir(), Options{})
	if err == nil {
		t.Fatal("expected error for nil connections")
	}
}

func TestRunNilSource(t *testing.T) {
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = dest.Close() }()
	_, runErr := Run(context.Background(), nil, dest, t.TempDir(), Options{})
	if runErr == nil {
		t.Fatal("expected error for nil source")
	}
}

func TestRunNilDest(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close() }()
	_, runErr := Run(context.Background(), source, nil, t.TempDir(), Options{})
	if runErr == nil {
		t.Fatal("expected error for nil dest")
	}
}

func TestRunNoEvents(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	orig := listDatabasesFn
	origRead := readCDCEventsFn
	defer func() {
		listDatabasesFn = orig
		readCDCEventsFn = origRead
	}()

	listDatabasesFn = func(_ context.Context, _ *sql.DB, _, _ []string) ([]string, error) {
		return []string{"testdb"}, nil
	}
	readCDCEventsFn = func(_ context.Context, _ *sql.DB, _ string, _ uint64, _ int) ([]CDCEvent, error) {
		return nil, nil
	}

	summary, err := Run(context.Background(), source, dest, t.TempDir(), Options{
		ConflictPolicy: "fail",
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if summary.AppliedEvents != 0 {
		t.Fatalf("expected 0 applied events, got %d", summary.AppliedEvents)
	}
}

func TestRunMaxEventsLimit(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	orig := listDatabasesFn
	origRead := readCDCEventsFn
	origPurge := purgeCDCEventsFn
	defer func() {
		listDatabasesFn = orig
		readCDCEventsFn = origRead
		purgeCDCEventsFn = origPurge
	}()

	listDatabasesFn = func(_ context.Context, _ *sql.DB, _, _ []string) ([]string, error) {
		return []string{"testdb"}, nil
	}
	readCDCEventsFn = func(_ context.Context, _ *sql.DB, _ string, _ uint64, _ int) ([]CDCEvent, error) {
		return []CDCEvent{
			{CDCID: 1, TableName: "orders", Operation: "INSERT", NewRowJSON: `{"id":1}`},
			{CDCID: 2, TableName: "orders", Operation: "INSERT", NewRowJSON: `{"id":2}`},
			{CDCID: 3, TableName: "orders", Operation: "INSERT", NewRowJSON: `{"id":3}`},
		}, nil
	}
	purgeCDCEventsFn = func(_ context.Context, _ *sql.DB, _ string, _ uint64) error {
		return nil
	}

	var applied []CDCEvent
	origApplyFn := applyEventFn
	applyEventFn = func(_ context.Context, _ *sql.DB, _ string, event CDCEvent, _ Options) error {
		applied = append(applied, event)
		return nil
	}
	defer func() { applyEventFn = origApplyFn }()

	var runErr error
	_, runErr = Run(context.Background(), source, dest, t.TempDir(), Options{
		ConflictPolicy: "fail",
		MaxEvents:      2,
	})
	if runErr != nil {
		t.Fatalf("expected success, got: %v", runErr)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 events applied (MaxEvents=2), got %d", len(applied))
	}
}

func TestRunApplyError(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	orig := listDatabasesFn
	origRead := readCDCEventsFn
	defer func() {
		listDatabasesFn = orig
		readCDCEventsFn = origRead
	}()

	listDatabasesFn = func(_ context.Context, _ *sql.DB, _, _ []string) ([]string, error) {
		return []string{"testdb"}, nil
	}
	readCDCEventsFn = func(_ context.Context, _ *sql.DB, _ string, _ uint64, _ int) ([]CDCEvent, error) {
		return []CDCEvent{
			{CDCID: 1, TableName: "orders", Operation: "INSERT", NewRowJSON: `{"id":1}`},
		}, nil
	}

	origApplyFn := applyEventFn
	applyEventFn = func(_ context.Context, _ *sql.DB, _ string, _ CDCEvent, _ Options) error {
		return errors.New("simulated apply error")
	}
	defer func() { applyEventFn = origApplyFn }()

	var applyRunErr error
	_, applyRunErr = Run(context.Background(), source, dest, t.TempDir(), Options{
		ConflictPolicy: "fail",
	})
	if applyRunErr == nil {
		t.Fatal("expected error from apply failure")
	}
	if !errors.Is(err, errors.New("simulated apply error")) && err.Error() == "" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterDatabases(t *testing.T) {
	all := []string{"app", "logs", "metrics", "archive"}

	got := filterDatabases(all, []string{"app", "logs"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 databases, got %d: %v", len(got), got)
	}

	got = filterDatabases(all, nil, []string{"archive"})
	if len(got) != 3 {
		t.Fatalf("expected 3 databases after exclude, got %d: %v", len(got), got)
	}

	got = filterDatabases(all, nil, nil)
	if len(got) != 4 {
		t.Fatalf("expected all 4 databases, got %d: %v", len(got), got)
	}
}

func TestBuildWhereFromRowAllNil(t *testing.T) {
	row := map[string]any{"id": nil}
	cols := []string{"id"}
	clause, args := buildWhereFromRow(row, cols)
	if clause != "`id` IS NULL" {
		t.Fatalf("unexpected clause for nil value: %q", clause)
	}
	if len(args) != 0 {
		t.Fatalf("expected no args for IS NULL clause, got %v", args)
	}
}

func TestBuildWhereFromRowMixed(t *testing.T) {
	row := map[string]any{"id": float64(1), "name": nil}
	cols := []string{"id", "name"}
	clause, args := buildWhereFromRow(row, cols)
	if len(args) != 1 {
		t.Fatalf("expected 1 arg, got %d", len(args))
	}
	_ = clause
}
