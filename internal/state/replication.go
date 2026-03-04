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
	Version    int       `json:"version"`
	BinlogFile string    `json:"binlog_file"`
	BinlogPos  uint32    `json:"binlog_pos"`
	ApplyDDL   string    `json:"apply_ddl"`
	UpdatedAt  time.Time `json:"updated_at"`
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
