package commands

import (
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
)

func TestBuildDataShapeFindings(t *testing.T) {
	report := dataShapePrecheckReport{
		KeylessTableCount: 1,
		KeylessTables: []keylessTableIssue{{
			Database: "app",
			Table:    "events",
			Proposal: "add a key",
		}},
		RepresentationRiskCount: 1,
		RepresentationRiskTables: []representationRiskIssue{{
			Database:                  "app",
			Table:                     "audit_log",
			ApproximateNumericColumns: 1,
			TemporalColumns:           1,
			JSONColumns:               1,
			CollationSensitiveColumns: 2,
			Proposal:                  "use canonical verify",
		}},
	}
	findings := buildDataShapeFindings(report)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"stable_key_required_tables_detected",
		"stable_key_required_table",
		"representation_sensitive_tables_detected",
		"representation_sensitive_table",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}

func TestBuildManualEvidenceFindings(t *testing.T) {
	report := manualEvidencePrecheckReport{
		SourceGrantsKnown:    true,
		SourceGrantCount:     2,
		HasReplicationClient: false,
		HasReplicationStream: false,
	}
	findings := buildManualEvidenceFindings(
		report,
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("11.4.8-MariaDB"),
		true,
	)
	codes := make([]string, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	joined := strings.Join(codes, ",")
	for _, code := range []string{
		"backup_restore_rehearsal_required",
		"metadata_lock_runbook_required",
		"replication_transaction_shape_rehearsal_required",
		"dump_tool_skew_review_required",
		"view_definer_rewrite_review_required",
		"source_current_user_grants_recorded",
		"replication_client_privilege_missing",
		"replication_stream_privilege_missing",
	} {
		if !strings.Contains(joined, code) {
			t.Fatalf("expected code %s in %#v", code, codes)
		}
	}
}
