package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/state"
	dataVerify "github.com/esthergb/dbmigrate/internal/verify/data"
)

func TestRunReportJSONIncludesArtifactsAndProposals(t *testing.T) {
	tmp := t.TempDir()
	dataPath := filepath.Join(tmp, "data-baseline-checkpoint.json")
	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	dataCheckpoint := state.NewDataCheckpoint()
	dataCheckpoint.Tables["app.users"] = state.TableCheckpoint{
		RowsCopied: 10,
		Done:       true,
		UpdatedAt:  time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC),
	}
	if err := state.SaveDataCheckpoint(dataPath, dataCheckpoint); err != nil {
		t.Fatalf("save data checkpoint: %v", err)
	}

	replicationCheckpoint := state.NewReplicationCheckpoint()
	replicationCheckpoint.BinlogFile = "mysql-bin.000031"
	replicationCheckpoint.BinlogPos = 456
	replicationCheckpoint.ApplyDDL = "warn"
	replicationCheckpoint.UpdatedAt = time.Date(2026, 3, 5, 12, 5, 0, 0, time.UTC)
	if err := state.SaveReplicationCheckpoint(replicationPath, replicationCheckpoint); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 12, 6, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.TableName = "app.users"
	conflictReport.Operation = "update"
	conflictReport.Message = "apply event failed"
	conflictReport.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
	conflictReport.RowDiffSample = []string{"name:old->new"}
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected report to fail by default when conflict report requires attention")
	}
	if !strings.Contains(err.Error(), "incompatible precheck or unresolved replication conflict artifacts") {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if payload.Summary.DataBaselineCheckpoint == nil {
		t.Fatal("expected data checkpoint in report")
	}
	if payload.Summary.DataBaselineCheckpoint.RowsCopied != 10 {
		t.Fatalf("unexpected rows copied: %d", payload.Summary.DataBaselineCheckpoint.RowsCopied)
	}
	if payload.Summary.ReplicationCheckpoint == nil {
		t.Fatal("expected replication checkpoint in report")
	}
	if payload.Summary.ReplicationCheckpoint.BinlogFile != "mysql-bin.000031" {
		t.Fatalf("unexpected binlog file: %q", payload.Summary.ReplicationCheckpoint.BinlogFile)
	}
	if payload.Summary.ReplicationConflictReport == nil {
		t.Fatal("expected conflict report in payload")
	}
	if payload.Summary.ReplicationConflictReport.FailureType != "schema_drift" {
		t.Fatalf("unexpected failure type: %q", payload.Summary.ReplicationConflictReport.FailureType)
	}
	if len(payload.Proposals) != 2 {
		t.Fatalf("unexpected proposals length: %d", len(payload.Proposals))
	}
	if !payload.Summary.ConflictOutputRedacted {
		t.Fatal("expected report output to redact plain conflict samples by default")
	}
	if len(payload.Summary.ReplicationConflictReport.RowDiffSample) != 0 {
		t.Fatalf("expected row diff sample to be redacted from report output, got %#v", payload.Summary.ReplicationConflictReport.RowDiffSample)
	}
	if !strings.Contains(strings.Join(payload.Proposals, " | "), "report output redacted plain-text conflict samples by default") {
		t.Fatalf("expected plain-text conflict proposal, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONCanIncludeSensitiveArtifacts(t *testing.T) {
	tmp := t.TempDir()
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 12, 6, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.Message = "apply event failed"
	conflictReport.RowDiffSample = []string{"name:old->new"}
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	_ = runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, []string{"--fail-on-conflict=false", "--include-sensitive-artifacts"}, &out)

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Summary.ConflictOutputRedacted {
		t.Fatal("did not expect sensitive output redaction when explicitly requested")
	}
	if len(payload.Summary.ReplicationConflictReport.RowDiffSample) != 1 {
		t.Fatalf("expected row diff sample to remain visible, got %#v", payload.Summary.ReplicationConflictReport.RowDiffSample)
	}
}

func TestRunReportJSONIncludesReplicationShapeProposals(t *testing.T) {
	tmp := t.TempDir()
	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")

	replicationCheckpoint := state.NewReplicationCheckpoint()
	replicationCheckpoint.BinlogFile = "mysql-bin.000031"
	replicationCheckpoint.BinlogPos = 456
	replicationCheckpoint.ApplyDDL = "warn"
	replicationCheckpoint.Shape = state.ReplicationTransactionShape{
		TransactionsSeen:     1,
		TransactionsApplied:  1,
		MaxTransactionEvents: 120,
		RiskLevel:            "high",
		RiskSignals: []string{
			"single_transaction_window",
			"large_transaction_dominates",
			"keyless_row_matching_pressure",
		},
	}
	replicationCheckpoint.UpdatedAt = time.Date(2026, 3, 5, 12, 5, 0, 0, time.UTC)
	if err := state.SaveReplicationCheckpoint(replicationPath, replicationCheckpoint); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out); err != nil {
		t.Fatalf("expected report without conflicts to succeed, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Summary.ReplicationCheckpoint == nil {
		t.Fatal("expected replication checkpoint in payload")
	}
	if payload.Summary.ReplicationCheckpoint.Shape.MaxTransactionEvents != 120 {
		t.Fatalf("unexpected shape summary: %+v", payload.Summary.ReplicationCheckpoint.Shape)
	}
	if len(payload.Proposals) == 0 {
		t.Fatal("expected shape proposals")
	}
	if !strings.Contains(strings.Join(payload.Proposals, " | "), "worker count will not split one huge commit") {
		t.Fatalf("expected chunking proposal, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONIncludesCollationArtifactAndProposals(t *testing.T) {
	tmp := t.TempDir()

	report := collationPrecheckReport{
		Name:                         "collation-compatibility",
		Incompatible:                 true,
		SourceVersion:                "8.4.8 MySQL Community Server - GPL",
		DestVersion:                  "10.6.17-MariaDB",
		SourceServerCollation:        "utf8mb4_0900_ai_ci",
		DestServerCollation:          "utf8mb4_general_ci",
		UnsupportedDestinationCount:  2,
		ClientCompatibilityRiskCount: 1,
	}
	if err := persistCollationPrecheckArtifact(tmp, report); err != nil {
		t.Fatalf("persist collation artifact: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected incompatible collation precheck to fail report by default")
	}
	if !strings.Contains(err.Error(), "incompatible precheck") {
		t.Fatalf("unexpected error: %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Summary.Artifacts.CollationPrecheck {
		t.Fatal("expected collation precheck artifact in summary")
	}
	if payload.Summary.CollationPrecheck == nil {
		t.Fatal("expected collation precheck summary")
	}
	if payload.Summary.CollationPrecheck.UnsupportedDestinationCount != 2 {
		t.Fatalf("unexpected unsupported destination count: %#v", payload.Summary.CollationPrecheck)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if len(payload.Proposals) != 2 {
		t.Fatalf("expected 2 collation proposals, got %#v", payload.Proposals)
	}
	if !strings.Contains(strings.Join(payload.Proposals, " | "), "server-side incompatibility") {
		t.Fatalf("expected server-side incompatibility proposal, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONIncludesPluginLifecycleArtifactAndProposals(t *testing.T) {
	tmp := t.TempDir()

	report := pluginLifecyclePrecheckReport{
		Name:         "plugin-lifecycle",
		Incompatible: true,
		UnsupportedStorageEngines: []storageEngineIssue{{
			Database:           "app",
			Table:              "audit_log",
			Engine:             "aria",
			DestinationSupport: "MISSING",
		}},
		UnsupportedAuthPlugins: []accountPluginIssue{{
			User:   "app_user",
			Host:   "%",
			Plugin: "mysql_native_password",
		}},
	}
	if err := persistPluginLifecyclePrecheckArtifact(tmp, report); err != nil {
		t.Fatalf("persist plugin lifecycle artifact: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected incompatible plugin lifecycle precheck to fail report by default")
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Summary.Artifacts.PluginLifecyclePrecheck {
		t.Fatal("expected plugin lifecycle precheck artifact in summary")
	}
	if payload.Summary.PluginLifecyclePrecheck == nil {
		t.Fatal("expected plugin lifecycle precheck summary")
	}
	if payload.Summary.PluginLifecyclePrecheck.UnsupportedEngineCount != 1 {
		t.Fatalf("unexpected engine count: %d", payload.Summary.PluginLifecyclePrecheck.UnsupportedEngineCount)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if len(payload.Proposals) != 2 {
		t.Fatalf("expected 2 plugin proposals, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONIncludesInvisibleGIPKArtifactAndProposals(t *testing.T) {
	tmp := t.TempDir()

	report := invisibleGIPKPrecheckReport{
		Name:                 "invisible-gipk",
		Incompatible:         true,
		InvisibleColumnCount: 1,
		GIPKTableCount:       1,
	}
	if err := persistInvisibleGIPKPrecheckArtifact(tmp, report); err != nil {
		t.Fatalf("persist invisible/gipk artifact: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected incompatible invisible/gipk precheck to fail report by default")
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Summary.Artifacts.InvisibleGIPKPrecheck {
		t.Fatal("expected invisible/gipk precheck artifact in summary")
	}
	if payload.Summary.InvisibleGIPKPrecheck == nil {
		t.Fatal("expected invisible/gipk precheck summary")
	}
	if payload.Summary.InvisibleGIPKPrecheck.GIPKTableCount != 1 {
		t.Fatalf("unexpected GIPK table count: %d", payload.Summary.InvisibleGIPKPrecheck.GIPKTableCount)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if len(payload.Proposals) != 1 {
		t.Fatalf("expected 1 invisible/gipk proposal, got %#v", payload.Proposals)
	}
	if !strings.Contains(payload.Proposals[0], "materialize or remove") {
		t.Fatalf("unexpected proposal: %q", payload.Proposals[0])
	}
}

func TestRunReportJSONAllowsIncompatiblePrecheckOverride(t *testing.T) {
	tmp := t.TempDir()

	report := collationPrecheckReport{
		Name:                        "collation-compatibility",
		Incompatible:                true,
		UnsupportedDestinationCount: 1,
	}
	if err := persistCollationPrecheckArtifact(tmp, report); err != nil {
		t.Fatalf("persist collation artifact: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, []string{"--fail-on-conflict=false"}, &out); err != nil {
		t.Fatalf("expected override to succeed, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
}

func TestRunReportJSONIncludesVerifyDataArtifactAndProposals(t *testing.T) {
	tmp := t.TempDir()

	artifact := verifyDataArtifact{
		Command:     "verify",
		Status:      "diff",
		VerifyLevel: "data",
		DataMode:    "sample",
		Summary: dataVerify.Summary{
			TablesCompared:           2,
			HashMismatches:           1,
			NoiseRiskMismatches:      1,
			RepresentationRiskTables: 2,
			Diffs: []dataVerify.Diff{{
				Kind:      "table_hash_mismatch",
				Database:  "app",
				Table:     "events",
				NoiseRisk: "representation_sensitive",
				Notes:     []string{"Temporal columns depend on session time_zone rendering; verify runs normalize sessions to UTC before hashing."},
			}},
		},
	}
	if err := persistVerifyDataArtifact(tmp, artifact.DataMode, artifact.Summary); err != nil {
		t.Fatalf("persist verify data artifact: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected verify diff artifact to fail report by default")
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Summary.Artifacts.VerifyDataReport {
		t.Fatal("expected verify data artifact in summary")
	}
	if payload.Summary.VerifyDataReport == nil {
		t.Fatal("expected verify data report summary")
	}
	if payload.Summary.VerifyDataReport.DiffCount != 1 {
		t.Fatalf("unexpected verify diff count: %#v", payload.Summary.VerifyDataReport)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if !strings.Contains(strings.Join(payload.Proposals, " | "), "representation-sensitive tables") {
		t.Fatalf("expected verify-data proposal, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONKeepsRiskOnlyVerifyArtifactOK(t *testing.T) {
	tmp := t.TempDir()

	artifact := verifyDataArtifact{
		Command:     "verify",
		Status:      "ok",
		VerifyLevel: "data",
		DataMode:    "full-hash",
		Summary: dataVerify.Summary{
			TablesCompared:           4,
			HashMismatches:           0,
			RepresentationRiskTables: 3,
			TableRisks: []dataVerify.TableRisk{{
				Database:                  "app",
				Table:                     "events",
				TemporalColumns:           1,
				CollationSensitiveColumns: 1,
				Notes: []string{
					"Temporal columns depend on session time_zone rendering; verify runs normalize sessions to UTC before hashing.",
					"Text ordering and collation differences can create false positives if hashing depends on SQL sort order; row hashes are sorted client-side here.",
				},
			}},
		},
	}
	if err := persistVerifyDataArtifact(tmp, artifact.DataMode, artifact.Summary); err != nil {
		t.Fatalf("persist verify data artifact: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out); err != nil {
		t.Fatalf("expected risk-only verify artifact to keep report ok, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if payload.Summary.VerifyDataReport == nil {
		t.Fatal("expected verify data report summary")
	}
	if payload.Summary.VerifyDataReport.DiffCount != 0 {
		t.Fatalf("expected no diffs, got %#v", payload.Summary.VerifyDataReport)
	}
	if len(payload.Proposals) != 1 {
		t.Fatalf("expected one risk-only proposal, got %#v", payload.Proposals)
	}
	if !strings.Contains(payload.Proposals[0], "representation-sensitive tables exist") {
		t.Fatalf("unexpected proposal: %#v", payload.Proposals)
	}
}

func TestRunReportJSONConflictOverrideDoesNotFail(t *testing.T) {
	tmp := t.TempDir()
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 12, 6, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.Message = "apply event failed"
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, []string{"--fail-on-conflict=false"}, &out); err != nil {
		t.Fatalf("expected report override to succeed, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
}

func TestRunReportJSONIgnoresStaleConflictWhenCheckpointAdvanced(t *testing.T) {
	tmp := t.TempDir()
	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	replicationCheckpoint := state.NewReplicationCheckpoint()
	replicationCheckpoint.BinlogFile = "mysql-bin.000200"
	replicationCheckpoint.BinlogPos = 900
	replicationCheckpoint.ApplyDDL = "warn"
	replicationCheckpoint.UpdatedAt = time.Date(2026, 3, 5, 13, 0, 0, 0, time.UTC)
	if err := state.SaveReplicationCheckpoint(replicationPath, replicationCheckpoint); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 12, 30, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.Message = "apply event failed"
	conflictReport.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
	conflictReport.AppliedEndFile = "mysql-bin.000200"
	conflictReport.AppliedEndPos = 800
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out); err != nil {
		t.Fatalf("expected stale conflict report to be ignored, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if !payload.Summary.ReplicationConflictStale {
		t.Fatal("expected replication conflict report to be marked stale")
	}
	if len(payload.Proposals) != 0 {
		t.Fatalf("expected no proposals for stale conflict report, got %#v", payload.Proposals)
	}
}

func TestRunReportJSONIgnoresStaleConflictByTimestampFallback(t *testing.T) {
	tmp := t.TempDir()
	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	replicationCheckpoint := state.NewReplicationCheckpoint()
	replicationCheckpoint.BinlogFile = "mysql-bin.000400"
	replicationCheckpoint.BinlogPos = 900
	replicationCheckpoint.ApplyDDL = "warn"
	replicationCheckpoint.UpdatedAt = time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	if err := state.SaveReplicationCheckpoint(replicationPath, replicationCheckpoint); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 13, 30, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.Message = "legacy conflict artifact without applied_end fields"
	conflictReport.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out); err != nil {
		t.Fatalf("expected stale conflict report to be ignored by timestamp fallback, got %v", err)
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if !payload.Summary.ReplicationConflictStale {
		t.Fatal("expected replication conflict report to be marked stale via timestamp fallback")
	}
}

func TestRunReportJSONKeepsConflictActiveWhenCheckpointOlderThanReport(t *testing.T) {
	tmp := t.TempDir()
	replicationPath := filepath.Join(tmp, "replication-checkpoint.json")
	conflictPath := filepath.Join(tmp, "replication-conflict-report.json")

	replicationCheckpoint := state.NewReplicationCheckpoint()
	replicationCheckpoint.BinlogFile = "mysql-bin.000500"
	replicationCheckpoint.BinlogPos = 900
	replicationCheckpoint.ApplyDDL = "warn"
	replicationCheckpoint.UpdatedAt = time.Date(2026, 3, 5, 14, 0, 0, 0, time.UTC)
	if err := state.SaveReplicationCheckpoint(replicationPath, replicationCheckpoint); err != nil {
		t.Fatalf("save replication checkpoint: %v", err)
	}

	conflictReport := state.NewReplicationConflictReport()
	conflictReport.GeneratedAt = time.Date(2026, 3, 5, 14, 5, 0, 0, time.UTC)
	conflictReport.FailureType = "schema_drift"
	conflictReport.Message = "new conflict after checkpoint"
	conflictReport.Remediation = "run migrate --schema-only to align schema, then rerun replicate"
	if err := state.SaveReplicationConflictReport(conflictPath, conflictReport); err != nil {
		t.Fatalf("save conflict report: %v", err)
	}

	var out bytes.Buffer
	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out)
	if err == nil {
		t.Fatal("expected report to fail for active conflict")
	}

	var payload reportResult
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Status != "attention_required" {
		t.Fatalf("unexpected status: %q", payload.Status)
	}
	if payload.Summary.ReplicationConflictStale {
		t.Fatal("did not expect conflict report to be stale")
	}
}

func TestRunReportTextNoArtifacts(t *testing.T) {
	tmp := t.TempDir()

	var out bytes.Buffer
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     false,
	}, nil, &out); err != nil {
		t.Fatalf("run report: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "status=empty") {
		t.Fatalf("expected empty status, got %q", text)
	}
	if !strings.Contains(text, "artifacts(collation_precheck=false plugin_lifecycle=false invisible_gipk=false verify_data=false data_baseline=false replication_checkpoint=false replication_conflict=false)") {
		t.Fatalf("expected artifact summary, got %q", text)
	}
}

func TestRunReportFailsOnInvalidConflictReportJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "replication-conflict-report.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("write conflict report file: %v", err)
	}

	err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for invalid conflict report json")
	}
	if !strings.Contains(err.Error(), "parse replication conflict report") {
		t.Fatalf("unexpected error: %v", err)
	}
}
