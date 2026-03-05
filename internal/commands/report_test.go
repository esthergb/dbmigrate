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
	if err := runReport(context.Background(), config.RuntimeConfig{
		StateDir: tmp,
		JSON:     true,
	}, nil, &out); err != nil {
		t.Fatalf("run report: %v", err)
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
	if len(payload.Proposals) != 1 {
		t.Fatalf("unexpected proposals length: %d", len(payload.Proposals))
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
	if !strings.Contains(text, "artifacts(data_baseline=false replication_checkpoint=false replication_conflict=false)") {
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
