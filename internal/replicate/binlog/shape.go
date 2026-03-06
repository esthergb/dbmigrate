package binlog

import (
	"sort"
	"strings"

	"github.com/esthergb/dbmigrate/internal/state"
)

type transactionShapeTracker struct {
	opts                      Options
	transactionsSeen          uint64
	transactionsApplied       uint64
	eventsSeen                uint64
	appliedEvents             uint64
	maxTransactionEvents      uint64
	ddlTransactions           uint64
	fkConstrainedTransactions uint64
	keylessOperations         uint64
	keylessTables             map[string]struct{}
}

func newTransactionShapeTracker(opts Options) *transactionShapeTracker {
	return &transactionShapeTracker{
		opts:          opts,
		keylessTables: map[string]struct{}{},
	}
}

func (t *transactionShapeTracker) observeBatch(batch applyBatch) {
	batchEvents := uint64(len(batch.Events))
	if batchEvents == 0 {
		return
	}

	t.transactionsSeen++
	t.eventsSeen += batchEvents
	if batchEvents > t.maxTransactionEvents {
		t.maxTransactionEvents = batchEvents
	}

	hasDDL := false
	hasFK := false
	for _, event := range batch.Events {
		if strings.EqualFold(strings.TrimSpace(event.Operation), "ddl") {
			hasDDL = true
		}
		if event.HasForeignKeys {
			hasFK = true
		}
		if event.UsesFallbackKey {
			t.keylessOperations++
			if strings.TrimSpace(event.TableName) != "" {
				t.keylessTables[event.TableName] = struct{}{}
			}
		}
	}
	if hasDDL {
		t.ddlTransactions++
	}
	if hasFK {
		t.fkConstrainedTransactions++
	}
}

func (t *transactionShapeTracker) markApplied(batch applyBatch) {
	batchEvents := uint64(len(batch.Events))
	if batchEvents == 0 {
		return
	}
	t.transactionsApplied++
	t.appliedEvents += batchEvents
}

func (t *transactionShapeTracker) snapshot() state.ReplicationTransactionShape {
	shape := state.ReplicationTransactionShape{
		TransactionsSeen:          t.transactionsSeen,
		TransactionsApplied:       t.transactionsApplied,
		EventsSeen:                t.eventsSeen,
		AppliedEvents:             t.appliedEvents,
		MaxTransactionEvents:      t.maxTransactionEvents,
		DDLTransactions:           t.ddlTransactions,
		FKConstrainedTransactions: t.fkConstrainedTransactions,
		KeylessOperations:         t.keylessOperations,
	}
	if t.transactionsSeen > 0 {
		shape.AvgTransactionEvents = t.eventsSeen / t.transactionsSeen
	}
	if len(t.keylessTables) > 0 {
		shape.KeylessTables = sortedSetKeys(t.keylessTables)
	}

	signals := make([]string, 0, 6)
	if t.transactionsSeen == 1 && t.eventsSeen > 1 {
		signals = append(signals, "single_transaction_window")
	}
	if shape.MaxTransactionEvents >= 50 &&
		(shape.TransactionsSeen == 1 || shape.MaxTransactionEvents >= maxUint64(shape.AvgTransactionEvents*5, 50)) {
		signals = append(signals, "large_transaction_dominates")
	}
	if shape.DDLTransactions > 0 {
		signals = append(signals, "ddl_serializes_apply")
	}
	if shape.FKConstrainedTransactions > 0 {
		signals = append(signals, "foreign_key_serialization_pressure")
	}
	if shape.KeylessOperations > 0 {
		signals = append(signals, "keyless_row_matching_pressure")
	}
	if t.opts.MaxEvents > 0 && shape.MaxTransactionEvents > t.opts.MaxEvents {
		signals = append(signals, "transaction_exceeds_max_events_limit")
	}
	if shape.TransactionsApplied < shape.TransactionsSeen && shape.TransactionsApplied > 0 {
		signals = append(signals, "window_cut_before_next_transaction")
	}
	shape.RiskSignals = signals
	shape.RiskLevel = shapeRiskLevel(signals)
	return shape
}

func shapeRiskLevel(signals []string) string {
	if len(signals) == 0 {
		return ""
	}

	highPriority := map[string]struct{}{
		"transaction_exceeds_max_events_limit": {},
		"ddl_serializes_apply":                 {},
		"large_transaction_dominates":          {},
		"foreign_key_serialization_pressure":   {},
		"keyless_row_matching_pressure":        {},
	}
	mediumPriority := map[string]struct{}{
		"window_cut_before_next_transaction": {},
		"single_transaction_window":          {},
	}

	for _, signal := range signals {
		if _, ok := highPriority[signal]; ok {
			return "high"
		}
	}
	for _, signal := range signals {
		if _, ok := mediumPriority[signal]; ok {
			return "medium"
		}
	}
	return "low"
}

func sortedSetKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func maxUint64(left uint64, right uint64) uint64 {
	if left > right {
		return left
	}
	return right
}
