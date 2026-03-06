package binlog

import (
	"testing"
)

func TestTransactionShapeTrackerSnapshotSignals(t *testing.T) {
	tracker := newTransactionShapeTracker(Options{MaxEvents: 20})
	tracker.observeBatch(applyBatch{
		EndFile: "mysql-bin.000001",
		EndPos:  100,
		Events: []applyEvent{
			{Operation: "update", TableName: "app.orders", HasForeignKeys: true},
			{Operation: "update", TableName: "app.orders", HasForeignKeys: true},
			{Operation: "update", TableName: "app.items", UsesFallbackKey: true},
			{Operation: "update", TableName: "app.items", UsesFallbackKey: true},
			{Operation: "ddl", TableName: "app"},
		},
	})
	tracker.observeBatch(applyBatch{
		EndFile: "mysql-bin.000001",
		EndPos:  160,
		Events:  []applyEvent{{Operation: "insert", TableName: "app.audit"}},
	})
	tracker.markApplied(applyBatch{
		EndFile: "mysql-bin.000001",
		EndPos:  100,
		Events: []applyEvent{
			{Operation: "update", TableName: "app.orders", HasForeignKeys: true},
			{Operation: "update", TableName: "app.orders", HasForeignKeys: true},
			{Operation: "update", TableName: "app.items", UsesFallbackKey: true},
			{Operation: "update", TableName: "app.items", UsesFallbackKey: true},
			{Operation: "ddl", TableName: "app"},
		},
	})

	shape := tracker.snapshot()
	if shape.TransactionsSeen != 2 {
		t.Fatalf("unexpected transactions seen: %d", shape.TransactionsSeen)
	}
	if shape.TransactionsApplied != 1 {
		t.Fatalf("unexpected transactions applied: %d", shape.TransactionsApplied)
	}
	if shape.MaxTransactionEvents != 5 {
		t.Fatalf("unexpected max transaction events: %d", shape.MaxTransactionEvents)
	}
	if shape.DDLTransactions != 1 {
		t.Fatalf("unexpected ddl transaction count: %d", shape.DDLTransactions)
	}
	if shape.FKConstrainedTransactions != 1 {
		t.Fatalf("unexpected fk transaction count: %d", shape.FKConstrainedTransactions)
	}
	if shape.KeylessOperations != 2 {
		t.Fatalf("unexpected keyless operations: %d", shape.KeylessOperations)
	}
	if shape.RiskLevel != "high" {
		t.Fatalf("unexpected risk level: %q", shape.RiskLevel)
	}
	assertHasSignal(t, shape.RiskSignals, "ddl_serializes_apply")
	assertHasSignal(t, shape.RiskSignals, "foreign_key_serialization_pressure")
	assertHasSignal(t, shape.RiskSignals, "keyless_row_matching_pressure")
	assertHasSignal(t, shape.RiskSignals, "window_cut_before_next_transaction")
}

func TestTransactionShapeTrackerFlagsLargeSingleTransaction(t *testing.T) {
	tracker := newTransactionShapeTracker(Options{})
	events := make([]applyEvent, 0, 60)
	for i := 0; i < 60; i++ {
		events = append(events, applyEvent{Operation: "insert", TableName: "app.bulk"})
	}
	tracker.observeBatch(applyBatch{Events: events})
	tracker.markApplied(applyBatch{Events: events})

	shape := tracker.snapshot()
	if shape.RiskLevel != "high" {
		t.Fatalf("unexpected risk level: %q", shape.RiskLevel)
	}
	assertHasSignal(t, shape.RiskSignals, "single_transaction_window")
	assertHasSignal(t, shape.RiskSignals, "large_transaction_dominates")
}

func assertHasSignal(t *testing.T, signals []string, want string) {
	t.Helper()
	for _, signal := range signals {
		if signal == want {
			return
		}
	}
	t.Fatalf("expected signal %q in %#v", want, signals)
}
