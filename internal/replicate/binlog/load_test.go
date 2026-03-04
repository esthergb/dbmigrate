package binlog

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestBuildApplyBatchesRowsWithXID(t *testing.T) {
	restore := stubLoadHooks(t)
	defer restore()

	loadTableMetadataFn = func(_ context.Context, _ *sql.DB, schema string, table string) (tableMetadata, error) {
		if schema != "app" || table != "items" {
			t.Fatalf("unexpected metadata lookup %s.%s", schema, table)
		}
		return tableMetadata{
			Columns:     []string{"id", "name"},
			KeyOrdinals: []int{0},
		}, nil
	}

	events := []streamEvent{
		{Kind: streamEventWriteRows, File: "mysql-bin.000001", Pos: 110, Schema: "app", Table: "items", Rows: [][]any{{int64(1), "a"}}},
		{Kind: streamEventXID, File: "mysql-bin.000001", Pos: 120},
		{Kind: streamEventUpdateRows, File: "mysql-bin.000001", Pos: 130, Schema: "app", Table: "items", Rows: [][]any{{int64(1), "a"}, {int64(1), "b"}}},
		{Kind: streamEventXID, File: "mysql-bin.000001", Pos: 140},
		{Kind: streamEventDeleteRows, File: "mysql-bin.000001", Pos: 150, Schema: "app", Table: "items", Rows: [][]any{{int64(1), "b"}}},
		{Kind: streamEventXID, File: "mysql-bin.000001", Pos: 160},
	}

	batches, err := buildApplyBatches(context.Background(), nil, events, Options{ApplyDDL: "warn"})
	if err != nil {
		t.Fatalf("buildApplyBatches: %v", err)
	}
	if len(batches) != 3 {
		t.Fatalf("unexpected batch count: %d", len(batches))
	}
	if batches[0].EndPos != 120 || len(batches[0].Events) != 1 {
		t.Fatalf("unexpected first batch: %+v", batches[0])
	}
	if !strings.Contains(batches[0].Events[0].Query, "INSERT INTO `app`.`items`") {
		t.Fatalf("unexpected upsert query: %s", batches[0].Events[0].Query)
	}
	if batches[1].EndPos != 140 || len(batches[1].Events) != 1 {
		t.Fatalf("unexpected second batch: %+v", batches[1])
	}
	if !strings.Contains(batches[1].Events[0].Query, "UPDATE `app`.`items`") {
		t.Fatalf("unexpected update query: %s", batches[1].Events[0].Query)
	}
	if batches[2].EndPos != 160 || len(batches[2].Events) != 1 {
		t.Fatalf("unexpected third batch: %+v", batches[2])
	}
	if !strings.Contains(batches[2].Events[0].Query, "DELETE FROM `app`.`items`") {
		t.Fatalf("unexpected delete query: %s", batches[2].Events[0].Query)
	}
}

func TestBuildApplyBatchesDDLWarnFails(t *testing.T) {
	events := []streamEvent{
		{Kind: streamEventQuery, File: "mysql-bin.000001", Pos: 200, Query: "ALTER TABLE app.items ADD COLUMN c INT"},
	}

	_, err := buildApplyBatches(context.Background(), nil, events, Options{ApplyDDL: "warn"})
	if err == nil {
		t.Fatal("expected DDL warn policy failure")
	}
	if !strings.Contains(err.Error(), "ddl encountered") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildApplyBatchesDDLApply(t *testing.T) {
	events := []streamEvent{
		{Kind: streamEventQuery, File: "mysql-bin.000001", Pos: 220, Query: "CREATE TABLE app.t(id INT PRIMARY KEY)"},
	}

	batches, err := buildApplyBatches(context.Background(), nil, events, Options{ApplyDDL: "apply"})
	if err != nil {
		t.Fatalf("buildApplyBatches: %v", err)
	}
	if len(batches) != 1 || len(batches[0].Events) != 1 {
		t.Fatalf("unexpected DDL apply batches: %+v", batches)
	}
	if batches[0].Events[0].Query != "CREATE TABLE app.t(id INT PRIMARY KEY)" {
		t.Fatalf("unexpected DDL query: %s", batches[0].Events[0].Query)
	}
}

func TestBuildApplyBatchesIncompleteTransactionFails(t *testing.T) {
	restore := stubLoadHooks(t)
	defer restore()

	loadTableMetadataFn = func(_ context.Context, _ *sql.DB, _ string, _ string) (tableMetadata, error) {
		return tableMetadata{
			Columns:     []string{"id"},
			KeyOrdinals: []int{0},
		}, nil
	}

	events := []streamEvent{
		{Kind: streamEventWriteRows, File: "mysql-bin.000010", Pos: 400, Schema: "app", Table: "items", Rows: [][]any{{int64(2)}}},
	}

	_, err := buildApplyBatches(context.Background(), nil, events, Options{ApplyDDL: "warn"})
	if err == nil {
		t.Fatal("expected incomplete transaction failure")
	}
	if !strings.Contains(err.Error(), "incomplete transaction") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadApplyBatchesFromSourceEmptyDSN(t *testing.T) {
	batches, err := loadApplyBatchesFromSource(context.Background(), nil, applyWindow{
		StartFile: "mysql-bin.000001",
		StartPos:  4,
		EndFile:   "mysql-bin.000001",
		EndPos:    40,
	}, Options{ApplyDDL: "warn"})
	if err != nil {
		t.Fatalf("loadApplyBatchesFromSource: %v", err)
	}
	if len(batches) != 0 {
		t.Fatalf("expected no batches, got %d", len(batches))
	}
}

func TestSourceSyncerConfigFlavorAndHost(t *testing.T) {
	mysqlCfg, err := sourceSyncerConfig("mysql://user:pass@127.0.0.1:3306/app?tls=preferred")
	if err != nil {
		t.Fatalf("sourceSyncerConfig mysql: %v", err)
	}
	if mysqlCfg.Flavor != "mysql" || mysqlCfg.Host != "127.0.0.1" || mysqlCfg.Port != 3306 {
		t.Fatalf("unexpected mysql sync config: %+v", mysqlCfg)
	}
	if mysqlCfg.ServerID == 0 {
		t.Fatal("expected non-zero server id")
	}

	mariaCfg, err := sourceSyncerConfig("mariadb://user:pass@db.example:3307/app")
	if err != nil {
		t.Fatalf("sourceSyncerConfig mariadb: %v", err)
	}
	if mariaCfg.Flavor != "mariadb" || mariaCfg.Host != "db.example" || mariaCfg.Port != 3307 {
		t.Fatalf("unexpected mariadb sync config: %+v", mariaCfg)
	}
}

func TestPositionHelpers(t *testing.T) {
	if !positionBefore("mysql-bin.000001", 4, "mysql-bin.000001", 8) {
		t.Fatal("expected positionBefore to be true")
	}
	if !positionAfter("mysql-bin.000003", 4, "mysql-bin.000002", 999) {
		t.Fatal("expected positionAfter to be true")
	}
	if !positionReachedOrPassed("mysql-bin.000002", 500, "mysql-bin.000002", 500) {
		t.Fatal("expected positionReachedOrPassed to be true")
	}
}

func stubLoadHooks(t *testing.T) func() {
	t.Helper()

	origStream := streamWindowEventsFn
	origMetadata := loadTableMetadataFn

	return func() {
		streamWindowEventsFn = origStream
		loadTableMetadataFn = origMetadata
	}
}
