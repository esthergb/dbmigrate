package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ReplicationConflictReport stores detailed failure context for replicate runs.
type ReplicationConflictReport struct {
	Version        int                         `json:"version"`
	GeneratedAt    time.Time                   `json:"generated_at"`
	ApplyDDL       string                      `json:"apply_ddl"`
	ConflictPolicy string                      `json:"conflict_policy"`
	StartFile      string                      `json:"start_file"`
	StartPos       uint32                      `json:"start_pos"`
	SourceEndFile  string                      `json:"source_end_file"`
	SourceEndPos   uint32                      `json:"source_end_pos"`
	AppliedEndFile string                      `json:"applied_end_file"`
	AppliedEndPos  uint32                      `json:"applied_end_pos"`
	FailureType    string                      `json:"failure_type"`
	SQLErrorCode   uint16                      `json:"sql_error_code,omitempty"`
	ValuesRedacted bool                        `json:"values_redacted,omitempty"`
	Operation      string                      `json:"operation,omitempty"`
	TableName      string                      `json:"table_name,omitempty"`
	Query          string                      `json:"query,omitempty"`
	ValueSample    []string                    `json:"value_sample,omitempty"`
	OldRowSample   []string                    `json:"old_row_sample,omitempty"`
	NewRowSample   []string                    `json:"new_row_sample,omitempty"`
	RowDiffSample  []string                    `json:"row_diff_sample,omitempty"`
	Shape          ReplicationTransactionShape `json:"shape,omitempty"`
	Message        string                      `json:"message"`
	Remediation    string                      `json:"remediation,omitempty"`
}

// NewReplicationConflictReport creates an empty conflict report value.
func NewReplicationConflictReport() ReplicationConflictReport {
	return ReplicationConflictReport{
		Version: checkpointVersion,
	}
}

// LoadReplicationConflictReport loads a conflict report file or returns default when missing.
func LoadReplicationConflictReport(path string) (ReplicationConflictReport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewReplicationConflictReport(), nil
		}
		return ReplicationConflictReport{}, fmt.Errorf("read replication conflict report: %w", err)
	}

	var report ReplicationConflictReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return ReplicationConflictReport{}, fmt.Errorf("parse replication conflict report: %w", err)
	}
	if report.Version == 0 {
		report.Version = checkpointVersion
	}
	return report, nil
}

// SaveReplicationConflictReport writes conflict report atomically.
func SaveReplicationConflictReport(path string, report ReplicationConflictReport) error {
	if report.Version == 0 {
		report.Version = checkpointVersion
	}

	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal replication conflict report: %w", err)
	}

	return writePrivateFileAtomic(path, raw)
}
