package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cp.json")

	cp := NewDataCheckpoint()
	cp.Tables["db1.t1"] = TableCheckpoint{RowsCopied: 42, Done: true, UpdatedAt: time.Now().UTC()}

	if err := SaveDataCheckpoint(path, cp); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	loaded, err := LoadDataCheckpoint(path)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}

	entry, ok := loaded.Tables["db1.t1"]
	if !ok {
		t.Fatal("expected checkpoint entry for db1.t1")
	}
	if entry.RowsCopied != 42 || !entry.Done {
		t.Fatalf("unexpected checkpoint entry: %#v", entry)
	}
}

func TestLoadMissingCheckpointReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.json")

	loaded, err := LoadDataCheckpoint(path)
	if err != nil {
		t.Fatalf("expected no error for missing checkpoint file: %v", err)
	}
	if len(loaded.Tables) != 0 {
		t.Fatalf("expected empty tables map, got %#v", loaded.Tables)
	}
}
