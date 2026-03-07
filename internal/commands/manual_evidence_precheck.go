package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/esthergb/dbmigrate/internal/compat"
)

type manualEvidencePrecheckReport struct {
	Name                 string           `json:"name"`
	SourceVersion        string           `json:"source_version"`
	DestVersion          string           `json:"dest_version"`
	SourceGrantsKnown    bool             `json:"source_grants_known"`
	SourceGrantCount     int              `json:"source_grant_count"`
	HasReplicationClient bool             `json:"has_replication_client"`
	HasReplicationStream bool             `json:"has_replication_stream"`
	Findings             []compat.Finding `json:"findings,omitempty"`
}

func runManualEvidencePrecheck(
	ctx context.Context,
	source *sql.DB,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
	includeViews bool,
) (manualEvidencePrecheckReport, error) {
	report := manualEvidencePrecheckReport{
		Name:          "manual-evidence",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
	}
	if source == nil {
		return report, fmt.Errorf("source connection is required")
	}
	grants, err := queryCurrentUserGrants(ctx, source)
	if err == nil {
		report.SourceGrantsKnown = true
		report.SourceGrantCount = len(grants)
		report.HasReplicationClient = grantsContainPrivilege(grants, "REPLICATION CLIENT")
		report.HasReplicationStream = grantsContainPrivilege(grants, "REPLICATION SLAVE") || grantsContainPrivilege(grants, "REPLICATION REPLICA")
	}
	report.Findings = buildManualEvidenceFindings(report, sourceInstance, destInstance, includeViews)
	return report, nil
}

func queryCurrentUserGrants(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER()")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 4)
	for rows.Next() {
		values := make([]sql.RawBytes, len(columns))
		scanArgs := make([]any, len(columns))
		for i := range values {
			scanArgs[i] = &values[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}
		if len(values) > 0 {
			out = append(out, string(values[0]))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func grantsContainPrivilege(grants []string, privilege string) bool {
	needle := strings.ToUpper(strings.TrimSpace(privilege))
	for _, grant := range grants {
		upper := strings.ToUpper(strings.TrimSpace(grant))
		if strings.Contains(upper, "ALL PRIVILEGES") || strings.Contains(upper, needle) {
			return true
		}
	}
	return false
}

func buildManualEvidenceFindings(report manualEvidencePrecheckReport, sourceInstance compat.Instance, destInstance compat.Instance, includeViews bool) []compat.Finding {
	findings := []compat.Finding{{
		Code:     "backup_restore_rehearsal_required",
		Severity: "warn",
		Message:  "Documented rollback safety still requires restore-usability evidence, not only backup completion evidence.",
		Proposal: "Run and retain a backup/restore rehearsal for the exact rollback workflow planned for this migration lane.",
	}, {
		Code:     "metadata_lock_runbook_required",
		Severity: "warn",
		Message:  "Metadata-lock incidents remain an operator risk during schema change windows and cannot be proven safe from static metadata alone.",
		Proposal: "Review the metadata-lock runbook and rehearse blocker identification before cutover windows that include DDL.",
	}, {
		Code:     "replication_transaction_shape_rehearsal_required",
		Severity: "warn",
		Message:  "Transaction-shape and large-commit lag risk remain operational concerns that static prechecks cannot fully prove safe.",
		Proposal: "Keep transaction-shape rehearsal evidence for the target workload instead of assuming worker count or chunk size alone will save the cutover.",
	}}
	if sourceInstance.Engine != destInstance.Engine || sourceInstance.Version != destInstance.Version {
		findings = append(findings, compat.Finding{
			Code:     "dump_tool_skew_review_required",
			Severity: "warn",
			Message:  "Logical dump/import tooling skew remains a documented risk when engines or major/minor lines differ.",
			Proposal: "Rehearse the exact dump/import client versions and flags planned for rollback or side-channel validation before cutover.",
		})
	}
	if includeViews {
		findings = append(findings, compat.Finding{
			Code:     "view_definer_rewrite_review_required",
			Severity: "warn",
			Message:  "View definers are sanitized during apply in v1, so operator review is still required when security context matters.",
			Proposal: "Review security expectations for views and verify that DEFINER=CURRENT_USER is acceptable on the destination.",
		})
	}
	if report.SourceGrantsKnown {
		findings = append(findings, compat.Finding{
			Code:     "source_current_user_grants_recorded",
			Severity: "info",
			Message:  fmt.Sprintf("Recorded %d source CURRENT_USER() grant row(s) for operational readiness review.", report.SourceGrantCount),
			Proposal: "Keep grant inventory with the plan artifact so privilege assumptions are explicit before the migration window.",
		})
		if !report.HasReplicationClient {
			findings = append(findings, compat.Finding{
				Code:     "replication_client_privilege_missing",
				Severity: "warn",
				Message:  "Source CURRENT_USER() grants do not clearly include REPLICATION CLIENT.",
				Proposal: "Grant REPLICATION CLIENT before relying on source binary-log status inventory or operational replication workflows.",
			})
		}
		if !report.HasReplicationStream {
			findings = append(findings, compat.Finding{
				Code:     "replication_stream_privilege_missing",
				Severity: "warn",
				Message:  "Source CURRENT_USER() grants do not clearly include REPLICATION SLAVE/REPLICA.",
				Proposal: "Grant the replication stream privilege before relying on incremental binlog replay or topology rehearsal.",
			})
		}
	} else {
		findings = append(findings, compat.Finding{
			Code:     "source_current_user_grants_unavailable",
			Severity: "warn",
			Message:  "Unable to inventory CURRENT_USER() grants from the source session.",
			Proposal: "Review source account grants manually, especially replication and metadata-read privileges, before cutover.",
		})
	}
	return findings
}
