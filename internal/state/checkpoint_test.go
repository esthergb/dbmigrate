package state

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestTableCheckpointTypedCursorRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 7, 9, 10, 11, 1200, time.UTC)
	original := []any{
		[]byte{0x00, 0x41},
		now,
		int64(-12),
		uint64(77),
		float64(1.25),
		true,
		"hello",
		nil,
	}

	var table TableCheckpoint
	if err := table.SetCursorValues(original); err != nil {
		t.Fatalf("set cursor values: %v", err)
	}
	if len(table.LastKey) != 0 {
		t.Fatalf("expected legacy last_key to be cleared, got %#v", table.LastKey)
	}
	if len(table.LastKeyTyped) != len(original) {
		t.Fatalf("unexpected typed cursor len: %d", len(table.LastKeyTyped))
	}

	decoded, err := table.CursorValues()
	if err != nil {
		t.Fatalf("decode cursor values: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("unexpected decoded len: %d", len(decoded))
	}

	if got, ok := decoded[0].([]byte); !ok || !bytes.Equal(got, []byte{0x00, 0x41}) {
		t.Fatalf("unexpected bytes decode: %#v", decoded[0])
	}
	if got, ok := decoded[1].(time.Time); !ok || !got.Equal(now) {
		t.Fatalf("unexpected time decode: %#v", decoded[1])
	}
	if got, ok := decoded[2].(int64); !ok || got != int64(-12) {
		t.Fatalf("unexpected int64 decode: %#v", decoded[2])
	}
	if got, ok := decoded[3].(uint64); !ok || got != uint64(77) {
		t.Fatalf("unexpected uint64 decode: %#v", decoded[3])
	}
	if got, ok := decoded[4].(float64); !ok || got != float64(1.25) {
		t.Fatalf("unexpected float decode: %#v", decoded[4])
	}
	if got, ok := decoded[5].(bool); !ok || !got {
		t.Fatalf("unexpected bool decode: %#v", decoded[5])
	}
	if got, ok := decoded[6].(string); !ok || got != "hello" {
		t.Fatalf("unexpected string decode: %#v", decoded[6])
	}
	if decoded[7] != nil {
		t.Fatalf("unexpected nil decode: %#v", decoded[7])
	}
}

func TestLoadDataCheckpointLegacyLastKeyIsUpgraded(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "legacy-cp.json")
	raw := []byte(`{
  "version": 1,
  "tables": {
    "app.users": {
      "rows_copied": 5,
      "key_columns": ["id"],
      "last_key": ["42"],
      "done": false,
      "updated_at": "2026-03-07T10:00:00Z"
    }
  }
}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write legacy checkpoint: %v", err)
	}

	cp, err := LoadDataCheckpoint(path)
	if err != nil {
		t.Fatalf("load legacy checkpoint: %v", err)
	}
	entry := cp.Tables["app.users"]
	if len(entry.LastKeyTyped) != 1 {
		t.Fatalf("expected upgraded typed cursor, got %#v", entry.LastKeyTyped)
	}
	values, err := entry.CursorValues()
	if err != nil {
		t.Fatalf("decode upgraded cursor: %v", err)
	}
	if len(values) != 1 || values[0] != "42" {
		t.Fatalf("unexpected legacy decoded values: %#v", values)
	}
}

func TestCheckpointCursorValuesInvalidTypeFails(t *testing.T) {
	entry := TableCheckpoint{
		LastKeyTyped: []CheckpointKeyValue{
			{Type: "unsupported", Value: "x"},
		},
	}
	if _, err := entry.CursorValues(); err == nil {
		t.Fatal("expected decode failure for unsupported cursor type")
	}
}

func TestAcquireDirLockBlocksSecondWriter(t *testing.T) {
	tmp := t.TempDir()

	lock, err := AcquireDirLock(tmp)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}
	defer func() {
		_ = lock.Release()
	}()

	if _, err := AcquireDirLock(tmp); err == nil {
		t.Fatal("expected second dir lock acquisition to fail")
	} else if !strings.Contains(err.Error(), ".dbmigrate.lock") || !strings.Contains(err.Error(), "remove the stale lock file manually") {
		t.Fatalf("expected operational stale-lock guidance, got %v", err)
	}
}
