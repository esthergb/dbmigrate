package data

import (
	"context"
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

	diffs, compared, missingDest, missingSource, mismatches, noiseRisk := diffTableHashes("app", source, dest, map[string]TableRisk{})
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
	if noiseRisk != 0 {
		t.Fatalf("expected noise risk mismatches=0, got %d", noiseRisk)
	}
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(diffs))
	}
}

func TestBuildSelectSQL(t *testing.T) {
	sql := buildSelectSQL("app", "users", []columnInfo{{Name: "id"}, {Name: "name"}})
	expected := "SELECT `id`, `name` FROM `app`.`users`"
	if sql != expected {
		t.Fatalf("unexpected select SQL: %s", sql)
	}
}

func TestBuildOrderedSelectSQL(t *testing.T) {
	sql := buildOrderedSelectSQL(
		"app",
		"users",
		[]columnInfo{{Name: "id"}, {Name: "name"}},
		[]string{"id"},
		true,
	)
	expected := "SELECT `id`, `name` FROM `app`.`users` ORDER BY `id` LIMIT ?"
	if sql != expected {
		t.Fatalf("unexpected ordered select SQL: %s", sql)
	}
}

func TestBuildKeysetSelectSQL(t *testing.T) {
	sql := buildKeysetSelectSQL(
		"app",
		"users",
		[]columnInfo{{Name: "id"}, {Name: "name"}},
		[]string{"id"},
		true,
	)
	expected := "SELECT `id`, `name` FROM `app`.`users` WHERE (`id`) > (?) ORDER BY `id` LIMIT ?"
	if sql != expected {
		t.Fatalf("unexpected keyset select SQL: %s", sql)
	}
}

func TestNormalizeHashValue(t *testing.T) {
	if normalizeHashValue(nil, columnInfo{}) != "null:" {
		t.Fatalf("expected null marker, got %q", normalizeHashValue(nil, columnInfo{}))
	}
	if normalizeHashValue([]byte("abc"), columnInfo{DataType: "blob"}) != "bytes:YWJj" {
		t.Fatalf("unexpected bytes marker: %q", normalizeHashValue([]byte("abc"), columnInfo{DataType: "blob"}))
	}
	if normalizeHashValue([]byte("{\"b\":2,\"a\":1}"), columnInfo{DataType: "json"}) != "json:{\"a\":1,\"b\":2}" {
		t.Fatalf("expected canonical json marker, got %q", normalizeHashValue([]byte("{\"b\":2,\"a\":1}"), columnInfo{DataType: "json"}))
	}
	if !strings.HasPrefix(normalizeHashValue(time.Date(2026, 3, 4, 12, 13, 14, 0, time.UTC), columnInfo{}), "time:2026-03-04T12:13:14Z") {
		t.Fatalf("unexpected time marker: %q", normalizeHashValue(time.Date(2026, 3, 4, 12, 13, 14, 0, time.UTC), columnInfo{}))
	}
}

func TestBuildRiskNotes(t *testing.T) {
	risk := TableRisk{
		ApproximateNumericColumns: 1,
		TemporalColumns:           1,
		JSONColumns:               1,
		CollationSensitiveColumns: 1,
	}
	notes := buildRiskNotes(risk)
	if len(notes) != 4 {
		t.Fatalf("expected 4 notes, got %d", len(notes))
	}
}

func TestVerifySampleDefaultsSampleSize(t *testing.T) {
	summary, err := VerifySample(context.TODO(), nil, nil, Options{})
	if err == nil {
		t.Fatalf("expected error for nil connections, got summary=%+v", summary)
	}
}

func TestVerifyFullHashRequiresConnections(t *testing.T) {
	summary, err := VerifyFullHash(context.TODO(), nil, nil, Options{})
	if err == nil {
		t.Fatalf("expected error for nil connections, got summary=%+v", summary)
	}
}

func TestIncompatibleStableKeyError(t *testing.T) {
	err := incompatibleStableKeyError("app", "events")
	if err == nil {
		t.Fatal("expected incompatible stable key error")
	}
	if !strings.Contains(err.Error(), "incompatible_for_v1_deterministic_hash") {
		t.Fatalf("unexpected error: %v", err)
	}
}
