package hybrid

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/esthergb/dbmigrate/internal/replicate/binlog"
	"github.com/esthergb/dbmigrate/internal/replicate/cdc"
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

func TestRunNoCDCNoBinlogDatabases(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	origCDC := cdcRunFn
	origBinlog := binlogRunFn
	defer func() {
		cdcRunFn = origCDC
		binlogRunFn = origBinlog
	}()

	cdcCalled := false
	cdcRunFn = func(_ context.Context, _, _ *sql.DB, _ string, _ cdc.Options) (cdc.Summary, error) {
		cdcCalled = true
		return cdc.Summary{}, nil
	}
	binlogCalled := false
	binlogRunFn = func(_ context.Context, _, _ *sql.DB, _ string, _ binlog.Options) (binlog.Summary, error) {
		binlogCalled = true
		return binlog.Summary{}, nil
	}

	_, runErr := Run(context.Background(), source, dest, t.TempDir(), Options{})
	if runErr != nil {
		t.Fatalf("expected success: %v", runErr)
	}
	if cdcCalled {
		t.Fatal("expected CDC not called when no CDCDatabases")
	}
	if !binlogCalled {
		t.Fatal("expected binlog called when no CDCDatabases and no BinlogDatabases")
	}
}

func TestRunCDCPhaseOnly(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	origCDC := cdcRunFn
	origBinlog := binlogRunFn
	defer func() {
		cdcRunFn = origCDC
		binlogRunFn = origBinlog
	}()

	cdcRunFn = func(_ context.Context, _, _ *sql.DB, _ string, _ cdc.Options) (cdc.Summary, error) {
		return cdc.Summary{AppliedEvents: 5}, nil
	}
	binlogCalled := false
	binlogRunFn = func(_ context.Context, _, _ *sql.DB, _ string, _ binlog.Options) (binlog.Summary, error) {
		binlogCalled = true
		return binlog.Summary{}, nil
	}

	summary, runErr := Run(context.Background(), source, dest, t.TempDir(), Options{
		CDCDatabases:    []string{"app"},
		BinlogDatabases: []string{"app"},
	})
	if runErr != nil {
		t.Fatalf("expected success: %v", runErr)
	}
	if summary.CDCSummary.AppliedEvents != 5 {
		t.Fatalf("expected 5 CDC applied events, got %d", summary.CDCSummary.AppliedEvents)
	}
	if !binlogCalled {
		t.Fatal("expected binlog also called for BinlogDatabases")
	}
}

func TestRunBinlogError(t *testing.T) {
	source, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	dest, err := sql.Open("mysql", "user:pass@tcp(127.0.0.1:1)/db")
	if err != nil {
		t.Skipf("sql.Open failed: %v", err)
	}
	defer func() { _ = source.Close(); _ = dest.Close() }()

	origBinlog := binlogRunFn
	defer func() { binlogRunFn = origBinlog }()

	binlogRunFn = func(_ context.Context, _, _ *sql.DB, _ string, _ binlog.Options) (binlog.Summary, error) {
		return binlog.Summary{}, errors.New("binlog failure")
	}

	_, runErr := Run(context.Background(), source, dest, t.TempDir(), Options{})
	if runErr == nil {
		t.Fatal("expected error from binlog phase")
	}
}

func TestParseTableRoutingEmpty(t *testing.T) {
	routing, err := ParseTableRouting("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routing) != 0 {
		t.Fatalf("expected empty routing, got %v", routing)
	}
}

func TestParseTableRoutingValid(t *testing.T) {
	routing, err := ParseTableRouting("app.orders=capture-triggers,app.events=binlog")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if routing["app.orders"] != TableModeCaptureTriggers {
		t.Fatalf("expected capture-triggers for app.orders, got %q", routing["app.orders"])
	}
	if routing["app.events"] != TableModeBinlog {
		t.Fatalf("expected binlog for app.events, got %q", routing["app.events"])
	}
}

func TestParseTableRoutingInvalidMode(t *testing.T) {
	_, err := ParseTableRouting("app.orders=stream")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestParseTableRoutingMalformed(t *testing.T) {
	_, err := ParseTableRouting("app.orders")
	if err == nil {
		t.Fatal("expected error for missing = separator")
	}
}

func TestDatabasesForModeEmptyRouting(t *testing.T) {
	all := []string{"app", "logs"}
	got := DatabasesForMode(nil, TableModeBinlog, all)
	if len(got) != 2 {
		t.Fatalf("expected all databases returned for empty routing, got %v", got)
	}
}

func TestDatabasesForModeFiltered(t *testing.T) {
	routing := TableRouting{
		"app.orders":  TableModeCaptureTriggers,
		"logs.events": TableModeBinlog,
	}
	cdcDBs := DatabasesForMode(routing, TableModeCaptureTriggers, nil)
	if len(cdcDBs) != 1 || cdcDBs[0] != "app" {
		t.Fatalf("expected [app] for CDC routing, got %v", cdcDBs)
	}
	binlogDBs := DatabasesForMode(routing, TableModeBinlog, nil)
	if len(binlogDBs) != 1 || binlogDBs[0] != "logs" {
		t.Fatalf("expected [logs] for binlog routing, got %v", binlogDBs)
	}
}
