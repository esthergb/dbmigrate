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
	if unsupported := unsupportedV1IncludeObjects(cfg.IncludeObjects); len(unsupported) > 0 {
		return WithExitCode(ExitCodeDiff, reservedV2ObjectsError(cfg.IncludeObjects))
	}

	_, err := db.NormalizeDSNWithTLS(cfg.Source, tlsOptionsFromRuntime(cfg))
	if err != nil {
		return fmt.Errorf("invalid source DSN: %w", err)
	}
	_, err = db.NormalizeDSNWithTLS(cfg.Dest, tlsOptionsFromRuntime(cfg))
	if err != nil {
		return fmt.Errorf("invalid dest DSN: %w", err)
	}

	if cfg.DryRun {
		return writeResult(out, cfg, "plan", "dry-run", "dry-run: compatibility precheck requires connectivity and is skipped")
	}

	return withStateDirLock(cfg, func() error {
		sourceDB, err := db.OpenAndPingWithTLS(ctx, cfg.Source, tlsOptionsFromRuntime(cfg))
		if err != nil {
			return fmt.Errorf("connect source: %w", err)
		}
		defer func() {
			_ = sourceDB.Close()
		}()

		destDB, err := db.OpenAndPingWithTLS(ctx, cfg.Dest, tlsOptionsFromRuntime(cfg))
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

		sourceInstance := compat.ParseInstance(sourceVersion)
		destInstance := compat.ParseInstance(destVersion)

		report := compat.Evaluate(
			sourceInstance,
			destInstance,
			cfg.Databases,
			cfg.DowngradeProfile,
		)

		precheckReport, err := runZeroDateDefaultsPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("schema precheck failed: %w", err)
		}
		if len(precheckReport.Findings) > 0 {
			report.Findings = append(report.Findings, precheckReport.Findings...)
		}
		if precheckReport.Incompatible {
			report.Compatible = false
		}

		pluginReport, err := runPluginLifecyclePrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("plugin lifecycle precheck failed: %w", err)
		}
		if len(pluginReport.Findings) > 0 {
			report.Findings = append(report.Findings, pluginReport.Findings...)
		}
		if pluginReport.Incompatible {
			report.Compatible = false
		}

		invisibleReport, err := runInvisibleGIPKPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("invisible/gipk precheck failed: %w", err)
		}
		if len(invisibleReport.Findings) > 0 {
			report.Findings = append(report.Findings, invisibleReport.Findings...)
		}
		if invisibleReport.Incompatible {
			report.Compatible = false
		}

		collationReport, err := runCollationPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("collation precheck failed: %w", err)
		}
		if len(collationReport.Findings) > 0 {
			report.Findings = append(report.Findings, collationReport.Findings...)
		}
		if collationReport.Incompatible {
			report.Compatible = false
		}

		schemaFeatureReport, err := runSchemaFeaturePrecheck(ctx, sourceDB, sourceInstance, destInstance, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("schema feature precheck failed: %w", err)
		}
		if len(schemaFeatureReport.Findings) > 0 {
			report.Findings = append(report.Findings, schemaFeatureReport.Findings...)
		}
		if schemaFeatureReport.Incompatible {
			report.Compatible = false
		}

		identifierReport, err := runIdentifierPortabilityPrecheck(ctx, sourceDB, destDB, sourceInstance, destInstance, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("identifier portability precheck failed: %w", err)
		}
		if len(identifierReport.Findings) > 0 {
			report.Findings = append(report.Findings, identifierReport.Findings...)
		}
		if identifierReport.Incompatible {
			report.Compatible = false
		}

		fkCycleReport, err := runForeignKeyCyclePrecheck(ctx, sourceDB, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("foreign-key cycle precheck failed: %w", err)
		}
		if len(fkCycleReport.Findings) > 0 {
			report.Findings = append(report.Findings, fkCycleReport.Findings...)
		}
		if fkCycleReport.IssueCount > 0 {
			report.Compatible = false
		}

		replicationBoundaryReport, err := runReplicationBoundaryPrecheck(ctx, sourceDB, destDB, sourceInstance, destInstance)
		if err != nil {
			return fmt.Errorf("replication boundary precheck failed: %w", err)
		}
		if len(replicationBoundaryReport.Findings) > 0 {
			report.Findings = append(report.Findings, replicationBoundaryReport.Findings...)
		}

		replicationReadinessReport, err := runReplicationReadinessPrecheck(ctx, sourceDB, destDB, sourceInstance, destInstance)
		if err != nil {
			return fmt.Errorf("replication readiness precheck failed: %w", err)
		}
		if len(replicationReadinessReport.Findings) > 0 {
			report.Findings = append(report.Findings, replicationReadinessReport.Findings...)
		}

		timezoneReport, err := runTimezonePortabilityPrecheck(ctx, sourceDB, destDB, cfg.Databases, cfg.ExcludeDatabases, sourceInstance, destInstance)
		if err != nil {
			return fmt.Errorf("timezone portability precheck failed: %w", err)
		}
		if len(timezoneReport.Findings) > 0 {
			report.Findings = append(report.Findings, timezoneReport.Findings...)
		}

		dataShapeReport, err := runDataShapePrecheck(ctx, sourceDB, cfg.Databases, cfg.ExcludeDatabases)
		if err != nil {
			return fmt.Errorf("data-shape precheck failed: %w", err)
		}
		if len(dataShapeReport.Findings) > 0 {
			report.Findings = append(report.Findings, dataShapeReport.Findings...)
		}
		if dataShapeReport.Incompatible {
			report.Compatible = false
		}

		manualEvidenceReport, err := runManualEvidencePrecheck(ctx, sourceDB, sourceInstance, destInstance, hasObject(cfg.IncludeObjects, "views"))
		if err != nil {
			return fmt.Errorf("manual evidence precheck failed: %w", err)
		}
		if len(manualEvidenceReport.Findings) > 0 {
			report.Findings = append(report.Findings, manualEvidenceReport.Findings...)
		}

		if err := writePlanReport(out, cfg, report); err != nil {
			return err
		}
		if !report.Compatible {
			return WithExitCode(
				ExitCodeDiff,
				errors.New("compatibility check failed; see detailed report with remediation proposals"),
			)
		}
		return nil
	})
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
		"[plan] status=%s profile=%s source=%s/%s dest=%s/%s downgrade=%v requires_evidence=%v findings=%d\n",
		status,
		report.DowngradeProfile,
		report.Source.Engine,
		report.Source.Version,
		report.Dest.Engine,
		report.Dest.Version,
		report.Downgrade,
		report.RequiresEvidence,
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
