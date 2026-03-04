package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReplicationCheckpointRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "replication-cp.json")

	cp := NewReplicationCheckpoint()
	cp.BinlogFile = "mysql-bin.000001"
	cp.BinlogPos = 1234
	cp.ApplyDDL = "warn"
	cp.UpdatedAt = time.Now().UTC()

	if err := SaveReplicationCheckpoint(path, cp); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	loaded, err := LoadReplicationCheckpoint(path)
	if err != nil {
		t.Fatalf("load replication checkpoint: %v", err)
	}
	if loaded.BinlogFile != cp.BinlogFile {
		t.Fatalf("unexpected binlog file: got=%q want=%q", loaded.BinlogFile, cp.BinlogFile)
	}
	if loaded.BinlogPos != cp.BinlogPos {
		t.Fatalf("unexpected binlog pos: got=%d want=%d", loaded.BinlogPos, cp.BinlogPos)
	}
	if loaded.ApplyDDL != cp.ApplyDDL {
		t.Fatalf("unexpected apply-ddl: got=%q want=%q", loaded.ApplyDDL, cp.ApplyDDL)
	}
}

func TestLoadMissingReplicationCheckpointReturnsDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing-replication-cp.json")

	loaded, err := LoadReplicationCheckpoint(path)
	if err != nil {
		t.Fatalf("expected no error for missing checkpoint file: %v", err)
	}
	if loaded.Version == 0 {
		t.Fatal("expected default checkpoint version to be set")
	}
	if loaded.ApplyDDL != "warn" {
		t.Fatalf("expected default apply-ddl warn, got %q", loaded.ApplyDDL)
	}
}
