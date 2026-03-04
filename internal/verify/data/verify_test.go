package data

import (
	"strings"
	"testing"
	"time"
)

func TestDiffTableCounts(t *testing.T) {
	source := map[string]int64{
		"users": 10,
		"posts": 25,
	}
	dest := map[string]int64{
		"users":    12,
		"comments": 4,
	}

	diffs, compared, missingDest, missingSource, mismatches := diffTableCounts("app", source, dest)
	if compared != 1 {
		t.Fatalf("expected compared=1, got %d", compared)
	}
	if missingDest != 1 {
		t.Fatalf("expected missing destination=1, got %d", missingDest)
	}
	if missingSource != 1 {
		t.Fatalf("expected missing source=1, got %d", missingSource)
	}
	if mismatches != 1 {
		t.Fatalf("expected count mismatches=1, got %d", mismatches)
	}
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(diffs))
	}
}

func TestUnionAndSort(t *testing.T) {
	values := unionAndSort([]string{"b", "a", " "}, []string{"a", "c"})
	expected := []string{"a", "b", "c"}
	if len(values) != len(expected) {
		t.Fatalf("unexpected union length: got=%d want=%d", len(values), len(expected))
	}
	for i := range expected {
		if values[i] != expected[i] {
			t.Fatalf("unexpected union item at %d: got=%q want=%q", i, values[i], expected[i])
		}
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	out := quoteIdentifier("na`me")
	if out != "`na``me`" {
		t.Fatalf("unexpected quoted identifier: %s", out)
	}
}

func TestDiffTableHashes(t *testing.T) {
	source := map[string]string{
		"users": "aaa",
		"posts": "bbb",
	}
	dest := map[string]string{
		"users": "ccc",
		"logs":  "ddd",
	}

	diffs, compared, missingDest, missingSource, mismatches := diffTableHashes("app", source, dest)
	if compared != 1 {
		t.Fatalf("expected compared=1, got %d", compared)
	}
	if missingDest != 1 {
		t.Fatalf("expected missing destination=1, got %d", missingDest)
	}
	if missingSource != 1 {
		t.Fatalf("expected missing source=1, got %d", missingSource)
	}
	if mismatches != 1 {
		t.Fatalf("expected hash mismatches=1, got %d", mismatches)
	}
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(diffs))
	}
}

func TestBuildOrderedSelectSQL(t *testing.T) {
	sql := buildOrderedSelectSQL("app", "users", []string{"id", "name"})
	expected := "SELECT `id`, `name` FROM `app`.`users` ORDER BY `id`, `name`"
	if sql != expected {
		t.Fatalf("unexpected select SQL: %s", sql)
	}
}

func TestNormalizeHashValue(t *testing.T) {
	if normalizeHashValue(nil) != "null:" {
		t.Fatalf("expected null marker, got %q", normalizeHashValue(nil))
	}
	if normalizeHashValue([]byte("abc")) != "bytes:YWJj" {
		t.Fatalf("unexpected bytes marker: %q", normalizeHashValue([]byte("abc")))
	}
	if !strings.HasPrefix(normalizeHashValue(time.Date(2026, 3, 4, 12, 13, 14, 0, time.UTC)), "time:2026-03-04T12:13:14Z") {
		t.Fatalf("unexpected time marker: %q", normalizeHashValue(time.Date(2026, 3, 4, 12, 13, 14, 0, time.UTC)))
	}
}
