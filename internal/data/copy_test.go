package data

import (
	"testing"
	"time"

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
		LastKey:    []string{"42"},
	}
	cursor := checkpointCursorArgs(progress, []string{"id"})
	if len(cursor) != 1 || cursor[0] != "42" {
		t.Fatalf("unexpected cursor: %#v", cursor)
	}

	mismatch := checkpointCursorArgs(progress, []string{"tenant_id"})
	if mismatch != nil {
		t.Fatalf("expected nil cursor when key columns mismatch, got %#v", mismatch)
	}
}

func TestKeyArgsToStringsNormalizesTypes(t *testing.T) {
	now := time.Date(2026, 3, 7, 12, 30, 0, 0, time.UTC)
	out := keyArgsToStrings([]any{[]byte("abc"), now, int64(7), nil})
	if len(out) != 4 {
		t.Fatalf("unexpected len: %d", len(out))
	}
	if out[0] != "abc" {
		t.Fatalf("unexpected bytes conversion: %q", out[0])
	}
	if out[1] != "2026-03-07T12:30:00Z" {
		t.Fatalf("unexpected time conversion: %q", out[1])
	}
	if out[2] != "7" {
		t.Fatalf("unexpected integer conversion: %q", out[2])
	}
	if out[3] != "" {
		t.Fatalf("unexpected nil conversion: %q", out[3])
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
