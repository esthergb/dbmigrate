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
	"strconv"
	"strings"
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
	ReplicationConflictStale    bool                             `json:"replication_conflict_stale,omitempty"`
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
	Path       string                            `json:"path"`
	BinlogFile string                            `json:"binlog_file"`
	BinlogPos  uint32                            `json:"binlog_pos"`
	ApplyDDL   string                            `json:"apply_ddl"`
	Shape      state.ReplicationTransactionShape `json:"shape,omitempty"`
	UpdatedAt  time.Time                         `json:"updated_at,omitempty"`
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
	if summary.ReplicationConflictReport != nil && summary.ReplicationConflictReport.FailureType != "" && !summary.ReplicationConflictStale {
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
		return WithExitCode(
			ExitCodeDiff,
			errors.New("report detected unresolved replication conflicts (use --fail-on-conflict=false to report without failing)"),
		)
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
			Shape:      cp.Shape,
			UpdatedAt:  cp.UpdatedAt,
		}
		proposals = append(proposals, replicationShapeProposals(cp.Shape)...)
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
		summary.ReplicationConflictStale = isStaleConflictReport(report, summary.ReplicationCheckpoint)
		if !summary.ReplicationConflictStale && report.Remediation != "" {
			proposals = append(proposals, report.Remediation)
		}
	}

	if len(proposals) == 0 && summary.Artifacts.ReplicationConflictReport && !summary.ReplicationConflictStale {
		proposals = append(proposals, "review replication-conflict-report.json and resolve destination drift before rerun")
	}
	return summary, proposals, nil
}

func isStaleConflictReport(report state.ReplicationConflictReport, checkpoint *replicationCheckpointSummary) bool {
	if checkpoint == nil {
		return false
	}
	if strings.TrimSpace(report.FailureType) == "" {
		return false
	}

	// Preferred path: compare checkpoint position against reported applied-end position.
	if strings.TrimSpace(report.AppliedEndFile) != "" && report.AppliedEndPos > 0 &&
		strings.TrimSpace(checkpoint.BinlogFile) != "" && checkpoint.BinlogPos > 0 {
		return positionAfter(checkpoint.BinlogFile, checkpoint.BinlogPos, report.AppliedEndFile, report.AppliedEndPos)
	}

	// Backward-compatible fallback for older conflict reports without applied-end position.
	if !checkpoint.UpdatedAt.IsZero() && !report.GeneratedAt.IsZero() {
		return checkpoint.UpdatedAt.After(report.GeneratedAt)
	}

	return false
}

func positionAfter(file string, pos uint32, targetFile string, targetPos uint32) bool {
	cmp := compareBinlogFile(file, targetFile)
	if cmp > 0 {
		return true
	}
	if cmp < 0 {
		return false
	}
	return pos > targetPos
}

func compareBinlogFile(left string, right string) int {
	if left == right {
		return 0
	}

	leftPrefix, leftNum, leftOK := splitBinlogFile(left)
	rightPrefix, rightNum, rightOK := splitBinlogFile(right)
	if leftOK && rightOK && leftPrefix == rightPrefix {
		if leftNum < rightNum {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}

func splitBinlogFile(name string) (string, uint64, bool) {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot >= len(name)-1 {
		return "", 0, false
	}
	prefix := name[:dot]
	suffix := name[dot+1:]
	number, err := strconv.ParseUint(suffix, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return prefix, number, true
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
			"[report] replication_checkpoint path=%s binlog=%s:%d apply_ddl=%s tx_shape(seen=%d applied=%d max_events=%d risk=%s signals=%s)\n",
			cp.Path,
			cp.BinlogFile,
			cp.BinlogPos,
			cp.ApplyDDL,
			cp.Shape.TransactionsSeen,
			cp.Shape.TransactionsApplied,
			cp.Shape.MaxTransactionEvents,
			cp.Shape.RiskLevel,
			strings.Join(cp.Shape.RiskSignals, ","),
		); err != nil {
			return err
		}
	}

	if payload.Summary.ReplicationConflictReport != nil {
		report := payload.Summary.ReplicationConflictReport
		if _, err := fmt.Fprintf(
			out,
			"[report] replication_conflict file=%s failure_type=%s stale=%v table=%s operation=%s message=%s\n",
			payload.Summary.ReplicationConflictFilePath,
			report.FailureType,
			payload.Summary.ReplicationConflictStale,
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

func replicationShapeProposals(shape state.ReplicationTransactionShape) []string {
	if len(shape.RiskSignals) == 0 {
		return nil
	}

	seen := map[string]struct{}{}
	proposals := make([]string, 0, len(shape.RiskSignals))
	add := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		if _, ok := seen[text]; ok {
			return
		}
		seen[text] = struct{}{}
		proposals = append(proposals, text)
	}

	for _, signal := range shape.RiskSignals {
		switch signal {
		case "large_transaction_dominates", "single_transaction_window":
			add("reduce source transaction size or chunk bulk changes; worker count will not split one huge commit.")
		case "transaction_exceeds_max_events_limit":
			add("increase --max-events or, preferably, reduce source transaction size so replication windows can advance cleanly.")
		case "ddl_serializes_apply":
			add("keep replication DDL outside hot catch-up windows or apply schema changes separately with migrate --schema-only.")
		case "foreign_key_serialization_pressure":
			add("review FK-heavy workloads and keep commit batches smaller so replica-style serialization pressure stays bounded.")
		case "keyless_row_matching_pressure":
			add("add stable primary keys where possible; fallback row matching increases apply cost and drift risk.")
		case "window_cut_before_next_transaction":
			add("expect checkpoint progress only at transaction boundaries; if windows look stalled, inspect transaction shape before tuning worker counts.")
		}
	}
	return proposals
}
