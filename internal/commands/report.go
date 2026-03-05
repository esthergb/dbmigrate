package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/state"
	"github.com/esthergb/dbmigrate/internal/version"
)

type reportResult struct {
	Command   string        `json:"command"`
	Status    string        `json:"status"`
	Summary   reportSummary `json:"summary"`
	Proposals []string      `json:"proposals,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
	Version   string        `json:"version"`
}

type reportSummary struct {
	StateDir                    string                           `json:"state_dir"`
	Artifacts                   reportArtifacts                  `json:"artifacts"`
	DataBaselineCheckpoint      *dataBaselineCheckpointSummary   `json:"data_baseline_checkpoint,omitempty"`
	ReplicationCheckpoint       *replicationCheckpointSummary    `json:"replication_checkpoint,omitempty"`
	ReplicationConflictReport   *state.ReplicationConflictReport `json:"replication_conflict_report,omitempty"`
	ReplicationConflictFilePath string                           `json:"replication_conflict_file,omitempty"`
}

type reportArtifacts struct {
	DataBaselineCheckpoint    bool `json:"data_baseline_checkpoint"`
	ReplicationCheckpoint     bool `json:"replication_checkpoint"`
	ReplicationConflictReport bool `json:"replication_conflict_report"`
}

type dataBaselineCheckpointSummary struct {
	Path       string    `json:"path"`
	Tables     int       `json:"tables"`
	Completed  int       `json:"completed"`
	RowsCopied int64     `json:"rows_copied"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type replicationCheckpointSummary struct {
	Path       string    `json:"path"`
	BinlogFile string    `json:"binlog_file"`
	BinlogPos  uint32    `json:"binlog_pos"`
	ApplyDDL   string    `json:"apply_ddl"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}

type reportOptions struct {
	FailOnConflict bool
}

func runReport(_ context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error {
	opts, err := parseReportOptions(args)
	if err != nil {
		return err
	}

	summary, proposals, err := loadReportSummary(cfg.StateDir)
	if err != nil {
		return err
	}

	status := "ok"
	if summary.ReplicationConflictReport != nil && summary.ReplicationConflictReport.FailureType != "" {
		status = "attention_required"
	}
	if !summary.Artifacts.DataBaselineCheckpoint && !summary.Artifacts.ReplicationCheckpoint && !summary.Artifacts.ReplicationConflictReport {
		status = "empty"
	}

	payload := reportResult{
		Command:   "report",
		Status:    status,
		Summary:   summary,
		Proposals: proposals,
		Timestamp: time.Now().UTC(),
		Version:   version.Version,
	}

	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			return err
		}
	} else {
		if err := writeReportText(out, payload); err != nil {
			return err
		}
	}

	if status == "attention_required" && opts.FailOnConflict {
		return errors.New("report detected unresolved replication conflicts (use --fail-on-conflict=false to report without failing)")
	}
	return nil
}

func parseReportOptions(args []string) (reportOptions, error) {
	opts := reportOptions{
		FailOnConflict: true,
	}

	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.FailOnConflict, "fail-on-conflict", true, "fail with non-zero exit when replication conflict report requires attention")
	if err := fs.Parse(args); err != nil {
		return reportOptions{}, err
	}
	return opts, nil
}

func loadReportSummary(stateDir string) (reportSummary, []string, error) {
	summary := reportSummary{
		StateDir: stateDir,
	}

	proposals := make([]string, 0, 2)

	dataCheckpointPath := filepath.Join(stateDir, "data-baseline-checkpoint.json")
	if exists, err := fileExists(dataCheckpointPath); err != nil {
		return reportSummary{}, nil, err
	} else if exists {
		cp, err := state.LoadDataCheckpoint(dataCheckpointPath)
		if err != nil {
			return reportSummary{}, nil, err
		}
		tables, completed, rowsCopied, updatedAt := summarizeDataCheckpoint(cp)
		summary.Artifacts.DataBaselineCheckpoint = true
		summary.DataBaselineCheckpoint = &dataBaselineCheckpointSummary{
			Path:       dataCheckpointPath,
			Tables:     tables,
			Completed:  completed,
			RowsCopied: rowsCopied,
			UpdatedAt:  updatedAt,
		}
	}

	replicationCheckpointPath := filepath.Join(stateDir, "replication-checkpoint.json")
	if exists, err := fileExists(replicationCheckpointPath); err != nil {
		return reportSummary{}, nil, err
	} else if exists {
		cp, err := state.LoadReplicationCheckpoint(replicationCheckpointPath)
		if err != nil {
			return reportSummary{}, nil, err
		}
		summary.Artifacts.ReplicationCheckpoint = true
		summary.ReplicationCheckpoint = &replicationCheckpointSummary{
			Path:       replicationCheckpointPath,
			BinlogFile: cp.BinlogFile,
			BinlogPos:  cp.BinlogPos,
			ApplyDDL:   cp.ApplyDDL,
			UpdatedAt:  cp.UpdatedAt,
		}
	}

	conflictReportPath := filepath.Join(stateDir, "replication-conflict-report.json")
	if exists, err := fileExists(conflictReportPath); err != nil {
		return reportSummary{}, nil, err
	} else if exists {
		report, err := state.LoadReplicationConflictReport(conflictReportPath)
		if err != nil {
			return reportSummary{}, nil, err
		}
		summary.Artifacts.ReplicationConflictReport = true
		summary.ReplicationConflictReport = &report
		summary.ReplicationConflictFilePath = conflictReportPath
		if report.Remediation != "" {
			proposals = append(proposals, report.Remediation)
		}
	}

	if len(proposals) == 0 && summary.Artifacts.ReplicationConflictReport {
		proposals = append(proposals, "review replication-conflict-report.json and resolve destination drift before rerun")
	}
	return summary, proposals, nil
}

func summarizeDataCheckpoint(cp state.DataCheckpoint) (int, int, int64, time.Time) {
	tables := len(cp.Tables)
	completed := 0
	var rowsCopied int64
	var updatedAt time.Time

	for _, entry := range cp.Tables {
		rowsCopied += entry.RowsCopied
		if entry.Done {
			completed++
		}
		if entry.UpdatedAt.After(updatedAt) {
			updatedAt = entry.UpdatedAt
		}
	}
	return tables, completed, rowsCopied, updatedAt
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("stat %s: %w", path, err)
}

func writeReportText(out io.Writer, payload reportResult) error {
	if _, err := fmt.Fprintf(
		out,
		"[report] status=%s state_dir=%s artifacts(data_baseline=%v replication_checkpoint=%v replication_conflict=%v) proposals=%d\n",
		payload.Status,
		payload.Summary.StateDir,
		payload.Summary.Artifacts.DataBaselineCheckpoint,
		payload.Summary.Artifacts.ReplicationCheckpoint,
		payload.Summary.Artifacts.ReplicationConflictReport,
		len(payload.Proposals),
	); err != nil {
		return err
	}

	if payload.Summary.DataBaselineCheckpoint != nil {
		cp := payload.Summary.DataBaselineCheckpoint
		if _, err := fmt.Fprintf(
			out,
			"[report] data_baseline path=%s tables=%d completed=%d rows=%d\n",
			cp.Path,
			cp.Tables,
			cp.Completed,
			cp.RowsCopied,
		); err != nil {
			return err
		}
	}

	if payload.Summary.ReplicationCheckpoint != nil {
		cp := payload.Summary.ReplicationCheckpoint
		if _, err := fmt.Fprintf(
			out,
			"[report] replication_checkpoint path=%s binlog=%s:%d apply_ddl=%s\n",
			cp.Path,
			cp.BinlogFile,
			cp.BinlogPos,
			cp.ApplyDDL,
		); err != nil {
			return err
		}
	}

	if payload.Summary.ReplicationConflictReport != nil {
		report := payload.Summary.ReplicationConflictReport
		if _, err := fmt.Fprintf(
			out,
			"[report] replication_conflict file=%s failure_type=%s table=%s operation=%s message=%s\n",
			payload.Summary.ReplicationConflictFilePath,
			report.FailureType,
			report.TableName,
			report.Operation,
			report.Message,
		); err != nil {
			return err
		}
	}

	for _, proposal := range payload.Proposals {
		if _, err := fmt.Fprintf(out, "[report] proposal=%s\n", proposal); err != nil {
			return err
		}
	}
	return nil
}
