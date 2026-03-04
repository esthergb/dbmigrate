package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/version"
)

type planResult struct {
	Command   string        `json:"command"`
	Status    string        `json:"status"`
	Report    compat.Report `json:"report"`
	Timestamp time.Time     `json:"timestamp"`
	Version   string        `json:"version"`
}

func runPlan(ctx context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("plan requires both --source and --dest (or config file equivalents)")
	}

	_, err := db.NormalizeDSN(cfg.Source)
	if err != nil {
		return fmt.Errorf("invalid source DSN: %w", err)
	}
	_, err = db.NormalizeDSN(cfg.Dest)
	if err != nil {
		return fmt.Errorf("invalid dest DSN: %w", err)
	}

	if cfg.DryRun {
		return writeResult(out, cfg, "plan", "dry-run: compatibility precheck requires connectivity and is skipped")
	}

	sourceDB, err := db.OpenAndPing(ctx, cfg.Source)
	if err != nil {
		return fmt.Errorf("connect source: %w", err)
	}
	defer func() {
		_ = sourceDB.Close()
	}()

	destDB, err := db.OpenAndPing(ctx, cfg.Dest)
	if err != nil {
		return fmt.Errorf("connect destination: %w", err)
	}
	defer func() {
		_ = destDB.Close()
	}()

	sourceVersion, err := queryServerVersion(ctx, sourceDB)
	if err != nil {
		return fmt.Errorf("detect source version: %w", err)
	}
	destVersion, err := queryServerVersion(ctx, destDB)
	if err != nil {
		return fmt.Errorf("detect destination version: %w", err)
	}

	report := compat.Evaluate(
		compat.ParseInstance(sourceVersion),
		compat.ParseInstance(destVersion),
		cfg.Databases,
		cfg.DowngradeProfile,
	)
	if err := writePlanReport(out, cfg, report); err != nil {
		return err
	}
	if !report.Compatible {
		return errors.New("compatibility check failed; see detailed report with remediation proposals")
	}
	return nil
}

func queryServerVersion(ctx context.Context, conn *sql.DB) (string, error) {
	var out string
	if err := conn.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out); err != nil {
		return "", err
	}
	if out == "" {
		return "", errors.New("empty VERSION() result")
	}
	return out, nil
}

func writePlanReport(out io.Writer, cfg config.RuntimeConfig, report compat.Report) error {
	status := "compatible"
	if !report.Compatible {
		status = "incompatible"
	}

	payload := planResult{
		Command:   "plan",
		Status:    status,
		Report:    report,
		Timestamp: time.Now().UTC(),
		Version:   version.Version,
	}
	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[plan] status=%s profile=%s source=%s/%s dest=%s/%s downgrade=%v findings=%d\n",
		status,
		report.DowngradeProfile,
		report.Source.Engine,
		report.Source.Version,
		report.Dest.Engine,
		report.Dest.Version,
		report.Downgrade,
		len(report.Findings),
	); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(
			out,
			"[plan] %s code=%s message=%s proposal=%s\n",
			finding.Severity,
			finding.Code,
			finding.Message,
			finding.Proposal,
		); err != nil {
			return err
		}
	}
	return nil
}
