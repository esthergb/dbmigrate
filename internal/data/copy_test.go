package data

import (
	"testing"

	"github.com/esthergb/dbmigrate/internal/state"
)

func TestBuildInsertSQL(t *testing.T) {
	sql := buildInsertSQL("app", "users", []string{"id", "name"})
	expected := "INSERT INTO `app`.`users` (`id`, `name`) VALUES (?, ?)"
	if sql != expected {
		t.Fatalf("unexpected insert SQL: %s", sql)
	}
}

func TestBuildSelectSQL(t *testing.T) {
	sql := buildSelectSQL("app", "users", []string{"id", "name"})
	expected := "SELECT `id`, `name` FROM `app`.`users` LIMIT ? OFFSET ?"
	if sql != expected {
		t.Fatalf("unexpected select SQL: %s", sql)
	}
}

func TestBuildKeysetSelectSQL(t *testing.T) {
	withoutCursor := buildKeysetSelectSQL("app", "users", []string{"id", "name"}, []string{"id"}, false)
	if withoutCursor != "SELECT `id`, `name` FROM `app`.`users` ORDER BY `id` LIMIT ?" {
		t.Fatalf("unexpected keyset SQL without cursor: %s", withoutCursor)
	}

	withCursor := buildKeysetSelectSQL("app", "users", []string{"id", "name"}, []string{"id"}, true)
	if withCursor != "SELECT `id`, `name` FROM `app`.`users` WHERE (`id`) > (?) ORDER BY `id` LIMIT ?" {
		t.Fatalf("unexpected keyset SQL with cursor: %s", withCursor)
	}
}

func TestCheckpointCursorArgsRequiresMatchingKeyColumns(t *testing.T) {
	progress := state.TableCheckpoint{
		KeyColumns: []string{"id"},
	}
	if err := progress.SetCursorValues([]any{int64(42)}); err != nil {
		t.Fatalf("set cursor values: %v", err)
	}
	cursor, err := checkpointCursorArgs(progress, []string{"id"})
	if err != nil {
		t.Fatalf("unexpected cursor decode error: %v", err)
	}
	if len(cursor) != 1 || cursor[0] != int64(42) {
		t.Fatalf("unexpected cursor: %#v", cursor)
	}

	mismatch, err := checkpointCursorArgs(progress, []string{"tenant_id"})
	if err != nil {
		t.Fatalf("unexpected mismatch error: %v", err)
	}
	if mismatch != nil {
		t.Fatalf("expected nil cursor when key columns mismatch, got %#v", mismatch)
	}
}

func TestCheckpointCursorArgsPropagatesDecodeFailure(t *testing.T) {
	progress := state.TableCheckpoint{
		KeyColumns: []string{"id"},
		LastKeyTyped: []state.CheckpointKeyValue{
			{Type: "unsupported", Value: "x"},
		},
	}
	_, err := checkpointCursorArgs(progress, []string{"id"})
	if err == nil {
		t.Fatal("expected decode error for unsupported checkpoint key type")
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	out := quoteIdentifier("na`me")
	if out != "`na``me`" {
		t.Fatalf("unexpected quoted identifier: %s", out)
	}
}

func TestCountPlaceholders(t *testing.T) {
	insertSQL := "INSERT INTO `app`.`users` (`id`, `name`, `email`) VALUES (?, ?, ?)"
	if got := countPlaceholders(insertSQL); got != 3 {
		t.Fatalf("expected 3 placeholders, got %d", got)
	}
}

func TestSortTableNamesByDependencies(t *testing.T) {
	tableNames := []string{"cart_items", "users", "orders"}
	dependencies := map[string]map[string]struct{}{
		"cart_items": {
			"users":  {},
			"orders": {},
		},
		"users":  {},
		"orders": {"users": {}},
	}

	got := sortTableNamesByDependencies(tableNames, dependencies)
	want := []string{"users", "orders", "cart_items"}

	if len(got) != len(want) {
		t.Fatalf("unexpected sorted length: got=%d want=%d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sorted order: got=%#v want=%#v", got, want)
		}
	}
}

func TestSortTableNamesByDependenciesCycleFallback(t *testing.T) {
	tableNames := []string{"b", "c", "a"}
	dependencies := map[string]map[string]struct{}{
		"a": {"b": {}},
		"b": {"a": {}},
		"c": {},
	}

	got := sortTableNamesByDependencies(tableNames, dependencies)
	want := []string{"c", "a", "b"}

	if len(got) != len(want) {
		t.Fatalf("unexpected sorted length: got=%d want=%d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sorted order: got=%#v want=%#v", got, want)
		}
	}
}
