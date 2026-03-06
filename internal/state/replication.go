package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ReplicationCheckpoint stores incremental replication progress.
type ReplicationCheckpoint struct {
	Version    int                         `json:"version"`
	BinlogFile string                      `json:"binlog_file"`
	BinlogPos  uint32                      `json:"binlog_pos"`
	ApplyDDL   string                      `json:"apply_ddl"`
	Shape      ReplicationTransactionShape `json:"shape,omitempty"`
	UpdatedAt  time.Time                   `json:"updated_at"`
}

// ReplicationTransactionShape captures transaction-shape and serialization risk signals
// observed while processing a replication window.
type ReplicationTransactionShape struct {
	TransactionsSeen          uint64   `json:"transactions_seen,omitempty"`
	TransactionsApplied       uint64   `json:"transactions_applied,omitempty"`
	EventsSeen                uint64   `json:"events_seen,omitempty"`
	AppliedEvents             uint64   `json:"applied_events,omitempty"`
	MaxTransactionEvents      uint64   `json:"max_transaction_events,omitempty"`
	AvgTransactionEvents      uint64   `json:"avg_transaction_events,omitempty"`
	DDLTransactions           uint64   `json:"ddl_transactions,omitempty"`
	FKConstrainedTransactions uint64   `json:"fk_constrained_transactions,omitempty"`
	KeylessOperations         uint64   `json:"keyless_operations,omitempty"`
	KeylessTables             []string `json:"keyless_tables,omitempty"`
	RiskLevel                 string   `json:"risk_level,omitempty"`
	RiskSignals               []string `json:"risk_signals,omitempty"`
}

// NewReplicationCheckpoint creates an empty checkpoint value.
func NewReplicationCheckpoint() ReplicationCheckpoint {
	return ReplicationCheckpoint{
		Version:  checkpointVersion,
		ApplyDDL: "warn",
	}
}

// LoadReplicationCheckpoint loads checkpoint file or returns default when missing.
func LoadReplicationCheckpoint(path string) (ReplicationCheckpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewReplicationCheckpoint(), nil
		}
		return ReplicationCheckpoint{}, fmt.Errorf("read replication checkpoint: %w", err)
	}

	var cp ReplicationCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return ReplicationCheckpoint{}, fmt.Errorf("parse replication checkpoint: %w", err)
	}
	if cp.Version == 0 {
		cp.Version = checkpointVersion
	}
	if cp.ApplyDDL == "" {
		cp.ApplyDDL = "warn"
	}
	return cp, nil
}

// SaveReplicationCheckpoint writes checkpoint atomically.
func SaveReplicationCheckpoint(path string, checkpoint ReplicationCheckpoint) error {
	if checkpoint.Version == 0 {
		checkpoint.Version = checkpointVersion
	}
	if checkpoint.ApplyDDL == "" {
		checkpoint.ApplyDDL = "warn"
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir replication checkpoint dir: %w", err)
	}

	raw, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal replication checkpoint: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write replication checkpoint temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace replication checkpoint: %w", err)
	}
	return nil
}
