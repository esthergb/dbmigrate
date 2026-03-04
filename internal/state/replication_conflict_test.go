package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestReplicationConflictReportRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "replication-conflict-report.json")

	report := NewReplicationConflictReport()
	report.GeneratedAt = time.Now().UTC()
	report.ApplyDDL = "warn"
	report.ConflictPolicy = "fail"
	report.StartFile = "mysql-bin.000001"
	report.StartPos = 4
	report.SourceEndFile = "mysql-bin.000001"
	report.SourceEndPos = 400
	report.AppliedEndFile = "mysql-bin.000001"
	report.AppliedEndPos = 320
	report.FailureType = "conflict"
	report.SQLErrorCode = 1062
	report.Operation = "update"
	report.TableName = "app.items"
	report.Query = "UPDATE `app`.`items` SET `name`=? WHERE `id` <=> ?"
	report.ValueSample = []string{"id=42", "name=legacy-name"}
	report.Message = "conflict-policy=fail detected non-applied update"
	report.Remediation = "rerun with source-wins after review"

	if err := SaveReplicationConflictReport(path, report); err != nil {
		t.Fatalf("save replication conflict report: %v", err)
	}

	loaded, err := LoadReplicationConflictReport(path)
	if err != nil {
		t.Fatalf("load replication conflict report: %v", err)
	}
	if loaded.FailureType != report.FailureType {
		t.Fatalf("unexpected failure type: got=%q want=%q", loaded.FailureType, report.FailureType)
	}
	if loaded.TableName != report.TableName {
		t.Fatalf("unexpected table name: got=%q want=%q", loaded.TableName, report.TableName)
	}
	if loaded.AppliedEndPos != report.AppliedEndPos {
		t.Fatalf("unexpected applied end pos: got=%d want=%d", loaded.AppliedEndPos, report.AppliedEndPos)
	}
	if loaded.SQLErrorCode != report.SQLErrorCode {
		t.Fatalf("unexpected sql error code: got=%d want=%d", loaded.SQLErrorCode, report.SQLErrorCode)
	}
	if len(loaded.ValueSample) != len(report.ValueSample) {
		t.Fatalf("unexpected value sample length: got=%d want=%d", len(loaded.ValueSample), len(report.ValueSample))
	}
	if loaded.ValueSample[0] != report.ValueSample[0] {
		t.Fatalf("unexpected first value sample: got=%q want=%q", loaded.ValueSample[0], report.ValueSample[0])
	}
}

func TestLoadMissingReplicationConflictReportReturnsDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing-replication-conflict-report.json")

	loaded, err := LoadReplicationConflictReport(path)
	if err != nil {
		t.Fatalf("expected no error for missing conflict report file: %v", err)
	}
	if loaded.Version == 0 {
		t.Fatal("expected default conflict report version to be set")
	}
}
