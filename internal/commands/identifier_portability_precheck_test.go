package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
)

func TestDetectReservedIdentifierIssues(t *testing.T) {
	destReserved := map[string]struct{}{"WINDOW": {}}
	sourceReserved := map[string]struct{}{}
	issues := detectReservedIdentifierIssues(
		[]string{"app"},
		[]inventoryObject{{Database: "app", ObjectType: "table", ObjectName: "window"}},
		[]inventoryColumn{{Database: "app", Table: "users", Column: "window"}},
		sourceReserved,
		destReserved,
	)
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %#v", issues)
	}
	if issues[0].Identifier != "window" || !strings.Contains(issues[0].Reason, "destination") {
		t.Fatalf("unexpected issue: %#v", issues[0])
	}
	if !issues[0].DestinationOnly || !issues[1].DestinationOnly {
		t.Fatalf("expected destination-only collisions, got %#v", issues)
	}
}

func TestDetectViewParserRiskIssues(t *testing.T) {
	issues := detectViewParserRiskIssues([]viewDefinition{{
		Database: "app",
		View:     "v_orders",
		Create:   "CREATE VIEW `v_orders` AS SELECT \"quoted\" AS label, col1 || col2 AS merged FROM `t1`",
	}}, "ANSI_QUOTES,PIPES_AS_CONCAT", "ONLY_FULL_GROUP_BY")
	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %#v", issues)
	}
	if !strings.Contains(issues[0].Risk, "ANSI_QUOTES") {
		t.Fatalf("unexpected first risk: %#v", issues[0])
	}
	if !strings.Contains(issues[1].Risk, "PIPES_AS_CONCAT") {
		t.Fatalf("unexpected second risk: %#v", issues[1])
	}
}

func TestBuildIdentifierPortabilityFindings(t *testing.T) {
	report := identifierPortabilityPrecheckReport{
		SourceLowerCaseKnown:      true,
		SourceLowerCaseTableNames: 0,
		DestLowerCaseKnown:        true,
		DestLowerCaseTableNames:   1,
		ReservedIdentifiers: []reservedIdentifierIssue{{
			Database:        "app",
			ObjectType:      "table",
			ObjectName:      "window",
			Identifier:      "window",
			DestinationOnly: true,
			Proposal:        "rename it",
		}},
		ReservedIdentifierCount: 1,
		CaseCollisions: []caseCollisionIssue{{
			Database:   "app",
			ObjectType: "table/view",
			FoldedName: "orders",
			Objects:    []string{"Orders", "orders"},
			Proposal:   "rename them",
		}},
		CaseCollisionCount: 1,
		MixedCaseIdentifiers: []mixedCaseIdentifierIssue{{
			Database:   "app",
			ObjectType: "table",
			ObjectName: "Orders",
			Proposal:   "normalize it",
		}},
		MixedCaseIdentifierCount: 1,
		ViewParserRisks: []viewParserRiskIssue{{
			Database: "app",
			View:     "v_orders",
			Risk:     "ANSI_QUOTES differs",
			Proposal: "rewrite it",
		}},
		ViewParserRiskCount: 1,
	}

	findings := buildIdentifierPortabilityFindings(report)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"lower_case_table_names_inventory_recorded",
		"lower_case_table_names_mismatch",
		"case_fold_collisions_detected",
		"mixed_case_identifiers_detected",
		"destination_reserved_identifiers_detected",
		"view_parser_drift_detected",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}

func TestBuildIdentifierPortabilityFindingsReservedWarnOnly(t *testing.T) {
	report := identifierPortabilityPrecheckReport{
		ReservedIdentifiers: []reservedIdentifierIssue{{
			Database:        "app",
			ObjectType:      "table",
			ObjectName:      "window",
			Identifier:      "window",
			DestinationOnly: false,
			Proposal:        "keep quoting it",
		}},
		ReservedIdentifierCount: 1,
	}

	findings := buildIdentifierPortabilityFindings(report)
	if len(findings) < 2 {
		t.Fatalf("expected summary + detail findings, got %#v", findings)
	}
	if findings[0].Code != "destination_reserved_identifiers_warn_only" || findings[0].Severity != "warn" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if findings[1].Code != "destination_reserved_identifier_warn_only" || findings[1].Severity != "warn" {
		t.Fatalf("unexpected detail finding: %#v", findings[1])
	}
}

func TestWriteIdentifierPortabilityPrecheckReportText(t *testing.T) {
	report := identifierPortabilityPrecheckReport{
		Name:                     "identifier-portability",
		Incompatible:             true,
		ReservedIdentifierCount:  1,
		ViewParserRiskCount:      1,
		CaseCollisionCount:       1,
		MixedCaseIdentifierCount: 1,
		Findings: []compat.Finding{{
			Code:     "destination_reserved_identifiers_detected",
			Severity: "error",
			Message:  "reserved drift",
			Proposal: "rename it",
		}},
	}
	var out bytes.Buffer
	if err := writeIdentifierPortabilityPrecheckReport(&out, config.RuntimeConfig{}, report); err != nil {
		t.Fatalf("write identifier precheck report: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "precheck=identifier-portability") {
		t.Fatalf("unexpected output: %q", text)
	}
	if !strings.Contains(text, "reserved_identifiers=1") || !strings.Contains(text, "parser_risks=1") {
		t.Fatalf("missing counters in output: %q", text)
	}
}

func TestBuildReplicationBoundaryFindings(t *testing.T) {
	report := replicationBoundaryPrecheckReport{
		CrossEngine:                        true,
		SourceLogBinKnown:                  true,
		SourceLogBin:                       false,
		SourceBinlogFormat:                 "STATEMENT",
		SourceGTIDMode:                     "ON",
		SourceGTIDPosition:                 "uuid:1-10",
		DestGTIDMode:                       "1",
		DestGTIDPosition:                   "0-1-10",
		SourceBinlogRowValueOptions:        "PARTIAL_JSON",
		SourceBinlogTransactionCompression: "ON",
	}
	findings := buildReplicationBoundaryFindings(
		report,
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("11.4.8-MariaDB"),
	)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"cross_engine_gtid_boundary_detected",
		"cross_engine_gtid_state_inventory_recorded",
		"cross_engine_replication_source_log_bin_disabled",
		"cross_engine_replication_non_row_binlog_format",
		"mysql_to_mariadb_binlog_row_value_options_set",
		"mysql_to_mariadb_binlog_transaction_compression_enabled",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}
