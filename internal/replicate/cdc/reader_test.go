package cdc

import (
	"context"
	"testing"
)

func TestParseJSONRowValid(t *testing.T) {
	m, err := ParseJSONRow(`{"id":1,"name":"alice"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["id"] == nil {
		t.Fatal("expected id in parsed row")
	}
	if m["name"] != "alice" {
		t.Fatalf("unexpected name: %v", m["name"])
	}
}

func TestParseJSONRowEmpty(t *testing.T) {
	m, err := ParseJSONRow("")
	if err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil map for empty input, got %v", m)
	}
}

func TestParseJSONRowInvalid(t *testing.T) {
	_, err := ParseJSONRow("not-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRowToValues(t *testing.T) {
	row := map[string]any{"id": float64(1), "name": "bob"}
	cols := []string{"id", "name"}
	vals := RowToValues(row, cols)
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[0] != float64(1) {
		t.Fatalf("unexpected id value: %v", vals[0])
	}
	if vals[1] != "bob" {
		t.Fatalf("unexpected name value: %v", vals[1])
	}
}

func TestRowToValuesMissingKey(t *testing.T) {
	row := map[string]any{"id": float64(1)}
	cols := []string{"id", "name"}
	vals := RowToValues(row, cols)
	if len(vals) != 2 {
		t.Fatalf("expected 2 values, got %d", len(vals))
	}
	if vals[1] != nil {
		t.Fatalf("expected nil for missing key, got %v", vals[1])
	}
}

func TestReadCDCEventsNilConnection(t *testing.T) {
	_, err := ReadCDCEvents(context.TODO(), nil, "testdb", 0, 100)
	if err == nil {
		t.Fatal("expected error for nil source connection")
	}
}

func TestPurgeCDCEventsNilConnection(t *testing.T) {
	err := PurgeCDCEvents(context.TODO(), nil, "testdb", 100)
	if err == nil {
		t.Fatal("expected error for nil source connection")
	}
}
