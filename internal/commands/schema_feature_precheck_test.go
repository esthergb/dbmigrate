package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
)

func TestBuildSchemaFeaturePrecheckFindingsCrossEngineJSON(t *testing.T) {
	report := schemaFeaturePrecheckReport{
		JSONColumnCount: 1,
		JSONColumns: []jsonColumnIssue{{
			Database: "app",
			Table:    "events",
			Column:   "payload",
			Proposal: "convert it",
		}},
	}

	findings := buildSchemaFeaturePrecheckFindings(
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("11.4.8-MariaDB"),
		report,
	)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %#v", findings)
	}
	if findings[0].Code != "cross_engine_json_columns_detected" || findings[0].Severity != "error" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if findings[1].Code != "cross_engine_json_column" {
		t.Fatalf("unexpected detail finding: %#v", findings[1])
	}
}

func TestBuildSchemaFeaturePrecheckFindingsMariaDBOnlyFeatures(t *testing.T) {
	report := schemaFeaturePrecheckReport{
		SequenceCount: 1,
		Sequences: []sequenceIssue{{
			Database: "app",
			Name:     "seq_orders",
			Proposal: "replace it",
		}},
		SystemVersionedTableCount: 1,
		SystemVersionedTables: []systemVersionedIssue{{
			Database: "app",
			Table:    "ledger",
			Proposal: "flatten it",
		}},
	}

	findings := buildSchemaFeaturePrecheckFindings(
		compat.ParseInstance("11.4.8-MariaDB"),
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		report,
	)
	if len(findings) != 4 {
		t.Fatalf("expected 4 findings, got %#v", findings)
	}
	if findings[0].Code != "mariadb_sequences_unsupported_on_destination" || findings[0].Severity != "error" {
		t.Fatalf("unexpected sequence summary: %#v", findings[0])
	}
	if findings[2].Code != "system_versioned_tables_unsupported_on_destination" || findings[2].Severity != "error" {
		t.Fatalf("unexpected system-versioned summary: %#v", findings[2])
	}
}

func TestBuildSchemaFeaturePrecheckFindingsClean(t *testing.T) {
	findings := buildSchemaFeaturePrecheckFindings(
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		schemaFeaturePrecheckReport{},
	)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %#v", findings)
	}
	if findings[0].Code != "schema_feature_inventory_clean" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}

func TestWriteSchemaFeaturePrecheckReportText(t *testing.T) {
	report := schemaFeaturePrecheckReport{
		Name:                      "schema-features",
		Incompatible:              true,
		JSONColumnCount:           1,
		SequenceCount:             1,
		SystemVersionedTableCount: 1,
		Findings: []compat.Finding{{
			Code:     "cross_engine_json_columns_detected",
			Severity: "error",
			Message:  "json mismatch",
			Proposal: "convert it",
		}},
	}

	var out bytes.Buffer
	if err := writeSchemaFeaturePrecheckReport(&out, config.RuntimeConfig{}, report); err != nil {
		t.Fatalf("write schema feature precheck report: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "precheck=schema-features") {
		t.Fatalf("unexpected text output: %q", text)
	}
	if !strings.Contains(text, "json_columns=1") || !strings.Contains(text, "system_versioned_tables=1") {
		t.Fatalf("expected counters in output: %q", text)
	}
	if !strings.Contains(text, "code=cross_engine_json_columns_detected") {
		t.Fatalf("expected finding line in output: %q", text)
	}
}
