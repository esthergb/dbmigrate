package data

import "testing"

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
