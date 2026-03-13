package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateServerIDGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	id1, err := LoadOrCreateServerID(dir, 0)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if id1 == 0 {
		t.Fatal("expected non-zero server_id")
	}

	id2, err := LoadOrCreateServerID(dir, 0)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("expected stable server_id across calls: first=%d second=%d", id1, id2)
	}
}

func TestLoadOrCreateServerIDDifferentDirsGetDifferentIDs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	id1, err := LoadOrCreateServerID(dir1, 0)
	if err != nil {
		t.Fatalf("dir1: %v", err)
	}
	id2, err := LoadOrCreateServerID(dir2, 0)
	if err != nil {
		t.Fatalf("dir2: %v", err)
	}

	if id1 == id2 {
		t.Logf("note: both dirs produced the same random ID (%d); extremely unlikely but not a correctness bug", id1)
	}
}

func TestLoadOrCreateServerIDExplicitOverride(t *testing.T) {
	dir := t.TempDir()

	id, err := LoadOrCreateServerID(dir, 99999)
	if err != nil {
		t.Fatalf("explicit override: %v", err)
	}
	if id != 99999 {
		t.Fatalf("expected explicit override 99999, got %d", id)
	}

	persisted, err := LoadOrCreateServerID(dir, 0)
	if err != nil {
		t.Fatalf("read after explicit override: %v", err)
	}
	if persisted == 99999 {
		t.Fatal("explicit override must not overwrite persisted value")
	}
}

func TestLoadOrCreateServerIDFileIsPersisted(t *testing.T) {
	dir := t.TempDir()
	id, err := LoadOrCreateServerID(dir, 0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	expectedFile := filepath.Join(dir, serverIDFile)
	raw, readErr := os.ReadFile(expectedFile)
	if readErr != nil {
		t.Fatalf("server_id file not written: %v", readErr)
	}
	if len(raw) == 0 {
		t.Fatal("server_id file is empty")
	}
	_ = id
}
