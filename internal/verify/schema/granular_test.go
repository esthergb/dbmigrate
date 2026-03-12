package schema

import (
	"context"
	"testing"
)

func TestVerifyGranularRequiresTables(t *testing.T) {
	_, err := VerifyGranular(context.Background(), nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error when tables not enabled")
	}
}

func TestVerifyGranularRequiresConnections(t *testing.T) {
	_, err := VerifyGranular(context.Background(), nil, nil, Options{IncludeTables: true})
	if err == nil {
		t.Fatal("expected error for nil connections")
	}
}

func TestSortGranularDiffs(t *testing.T) {
	diffs := []GranularDiff{
		{Kind: granularKindColumnMissing, Database: "db1", Table: "z_table", ObjectName: "col_a"},
		{Kind: granularKindIndexMissing, Database: "db1", Table: "a_table", ObjectName: "idx_b"},
		{Kind: granularKindFKMissing, Database: "db1", Table: "a_table", ObjectName: "fk_c"},
	}
	sortGranularDiffs(diffs)

	if diffs[0].Table != "a_table" {
		t.Fatalf("expected a_table first, got %q", diffs[0].Table)
	}
	if diffs[1].Table != "a_table" {
		t.Fatalf("expected second entry a_table, got %q", diffs[1].Table)
	}
	if diffs[2].Table != "z_table" {
		t.Fatalf("expected z_table last, got %q", diffs[2].Table)
	}
}

func TestGranularKindConstants(t *testing.T) {
	cases := map[string]string{
		granularKindColumnMissing:     "column_missing_in_destination",
		granularKindColumnExtra:       "column_extra_in_destination",
		granularKindColumnMismatch:    "column_definition_mismatch",
		granularKindIndexMissing:      "index_missing_in_destination",
		granularKindIndexExtra:        "index_extra_in_destination",
		granularKindIndexMismatch:     "index_definition_mismatch",
		granularKindFKMissing:         "fk_missing_in_destination",
		granularKindFKExtra:           "fk_extra_in_destination",
		granularKindFKMismatch:        "fk_definition_mismatch",
		granularKindPartitionMissing:  "partition_missing_in_destination",
		granularKindPartitionExtra:    "partition_extra_in_destination",
		granularKindPartitionMismatch: "partition_definition_mismatch",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("constant mismatch: got %q want %q", got, want)
		}
	}
}

func TestColumnDefSignature(t *testing.T) {
	c := columnDef{
		name:       "id",
		colType:    "BIGINT",
		isNullable: "NO",
	}
	sig := c.signature()
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	if normalizeStr("BIGINT") != "bigint" {
		t.Fatal("normalizeStr should lowercase")
	}
}

func TestToSetAndUnionStrings(t *testing.T) {
	s := toSet([]string{"a", "b", "c"})
	if _, ok := s["a"]; !ok {
		t.Fatal("expected a in set")
	}
	if _, ok := s["x"]; ok {
		t.Fatal("unexpected x in set")
	}

	u := unionStrings([]string{"b", "a"}, []string{"c", "a"})
	if len(u) != 3 {
		t.Fatalf("expected 3 items in union, got %d", len(u))
	}
	if u[0] != "a" || u[1] != "b" || u[2] != "c" {
		t.Fatalf("unexpected union order: %v", u)
	}
}
