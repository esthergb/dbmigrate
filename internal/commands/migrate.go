package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/data"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/version"
)

type migrateOptions struct {
	SchemaOnly        bool
	DataOnly          bool
	DestEmptyRequired bool
	Force             bool
	ChunkSize         int
	Resume            bool
}

type migrateDryRunSandboxResult struct {
	Command       string    `json:"command"`
	Status        string    `json:"status"`
	DryRunMode    string    `json:"dry_run_mode"`
	Validated     int       `json:"validated"`
	Failed        int       `json:"failed"`
	CleanupStatus string    `json:"cleanup_status"`
	Message       string    `json:"message,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	Version       string    `json:"version"`
}

func runMigrate(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error {
	opts, err := parseMigrateOptions(args)
	if err != nil {
		return err
	}
	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("migrate requires both --source and --dest (or config file equivalents)")
	}
	if opts.SchemaOnly && opts.DataOnly {
		return errors.New("--schema-only and --data-only cannot be used together")
	}

	runSchema := opts.SchemaOnly || (!opts.SchemaOnly && !opts.DataOnly)
	runData := opts.DataOnly || (!opts.SchemaOnly && !opts.DataOnly)
	if unsupported := unsupportedV1IncludeObjects(cfg.IncludeObjects); len(unsupported) > 0 {
		return WithExitCode(ExitCodeDiff, reservedV2ObjectsError(cfg.IncludeObjects))
	}
	includeTables := hasObject(cfg.IncludeObjects, "tables")
	includeViews := hasObject(cfg.IncludeObjects, "views")

	if cfg.DryRun {
		if cfg.DryRunMode == "sandbox" {
			return runMigrateDryRunSandbox(ctx, cfg, runSchema, runData, includeTables, includeViews, opts, out)
		}
		message := fmt.Sprintf(
			"dry-run: migrate plan ready (schema=%v data=%v chunk_size=%d resume=%v)",
			runSchema,
			runData,
			opts.ChunkSize,
			opts.Resume,
		)
		return writeResult(out, cfg, "migrate", "dry-run", message)
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

		if runSchema {
			compatReport := compat.Evaluate(
				sourceInstance,
				destInstance,
				cfg.Databases,
				cfg.DowngradeProfile,
			)
			if !compatReport.Compatible {
				return WithExitCode(
					ExitCodeDiff,
					errors.New("version compatibility check failed; run plan for detailed report with remediation proposals"),
				)
			}

			var precheckWarnings []compat.Finding
			for _, f := range compatReport.Findings {
				if f.Severity == "warn" || f.Severity == "info" {
					precheckWarnings = append(precheckWarnings, f)
				}
			}

			pluginReport, err := runPluginLifecyclePrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("plugin lifecycle precheck failed: %w", err)
			}
			if pluginReport.Incompatible {
				if err := writePluginLifecyclePrecheckReport(out, cfg, pluginReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; source uses storage engines that are unsupported on destination"),
				)
			}
			for _, f := range pluginReport.Findings {
				if f.Severity == "warn" || f.Severity == "info" {
					precheckWarnings = append(precheckWarnings, f)
				}
			}

			invisibleReport, err := runInvisibleGIPKPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("invisible/gipk precheck failed: %w", err)
			}
			if invisibleReport.Incompatible {
				if err := writeInvisibleGIPKPrecheckReport(out, cfg, invisibleReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; invisible columns or generated invisible primary keys drift on destination"),
				)
			}
			for _, f := range invisibleReport.Findings {
				if f.Severity == "warn" || f.Severity == "info" {
					precheckWarnings = append(precheckWarnings, f)
				}
			}

			collationReport, err := runCollationPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("collation precheck failed: %w", err)
			}
			if collationReport.Incompatible {
				if err := writeCollationMigratePrecheckReport(out, cfg, collationReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; source collations are unsupported on destination"),
				)
			}
			for _, f := range collationReport.Findings {
				if f.Severity == "warn" || f.Severity == "info" {
					precheckWarnings = append(precheckWarnings, f)
				}
			}

			precheckReport, err := runZeroDateDefaultsPrecheck(ctx, sourceDB, destDB, cfg.StateDir, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("schema precheck failed: %w", err)
			}
			if precheckReport.Incompatible {
				if err := writeMigratePrecheckReport(out, cfg, precheckReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; zero-date defaults are incompatible with destination sql_mode"),
				)
			}
			for _, f := range precheckReport.Findings {
				if f.Severity == "warn" || f.Severity == "info" {
					precheckWarnings = append(precheckWarnings, f)
				}
			}

			schemaFeatureReport, err := runSchemaFeaturePrecheck(ctx, sourceDB, sourceInstance, destInstance, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("schema feature precheck failed: %w", err)
			}
			if schemaFeatureReport.Incompatible {
				if err := writeSchemaFeaturePrecheckReport(out, cfg, schemaFeatureReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; source uses schema features that are incompatible with destination in v1"),
				)
			}

			identifierReport, err := runIdentifierPortabilityPrecheck(ctx, sourceDB, destDB, sourceInstance, destInstance, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("identifier portability precheck failed: %w", err)
			}
			if identifierReport.Incompatible {
				if err := writeIdentifierPortabilityPrecheckReport(out, cfg, identifierReport); err != nil {
					return err
				}
				return WithExitCode(
					ExitCodeDiff,
					errors.New("schema precheck failed; identifier portability or parser drift is incompatible with destination in v1"),
				)
			}

			for _, w := range precheckWarnings {
				cfg.Log.Warn("precheck finding", "code", w.Code, "severity", w.Severity, "message", w.Message)
			}
		}

		schemaSummary := schema.CopySummary{}
		if runSchema {
			schemaOptions := schema.CopyOptions{
				IncludeDatabases:  cfg.Databases,
				ExcludeDatabases:  cfg.ExcludeDatabases,
				IncludeTables:     includeTables,
				IncludeViews:      includeViews,
				DestEmptyRequired: opts.DestEmptyRequired && !opts.Force,
				Log:               cfg.Log,
			}
			if !schemaOptions.IncludeTables && !schemaOptions.IncludeViews {
				return errors.New("schema migration currently supports tables/views in --include-objects")
			}

			schemaSummary, err = schema.CopySchema(ctx, sourceDB, destDB, schemaOptions)
			if err != nil {
				if isMigrateCompatibilityError(err) {
					return WithExitCode(ExitCodeDiff, fmt.Errorf("schema migration failed: %w", err))
				}
				return fmt.Errorf("schema migration failed: %w", err)
			}
		}

		dataSummary := data.CopySummary{}
		if runData {
			if !hasObject(cfg.IncludeObjects, "tables") {
				return errors.New("data migration requires tables in --include-objects")
			}
			dataShapeReport, err := runDataShapePrecheck(ctx, sourceDB, cfg.Databases, cfg.ExcludeDatabases)
			if err != nil {
				return fmt.Errorf("data-shape precheck failed: %w", err)
			}
			if dataShapeReport.Incompatible {
				return WithExitCode(
					ExitCodeDiff,
					errors.New("data precheck failed; selected scope contains tables without stable keys required for v1 baseline migration"),
				)
			}
			dataOptions := data.CopyOptions{
				IncludeDatabases: cfg.Databases,
				ExcludeDatabases: cfg.ExcludeDatabases,
				ChunkSize:        opts.ChunkSize,
				Resume:           opts.Resume,
				RequireEmptyDest: runData && !runSchema && opts.DestEmptyRequired && !opts.Force,
				Log:              cfg.Log,
			}

			dataSummary, err = data.CopyBaselineData(ctx, sourceDB, destDB, cfg.StateDir, dataOptions)
			if err != nil {
				if isMigrateCompatibilityError(err) {
					return WithExitCode(ExitCodeDiff, fmt.Errorf("data migration failed: %w", err))
				}
				return fmt.Errorf("data migration failed: %w", err)
			}
		}

		message := fmt.Sprintf(
			"migration completed: schema(databases=%d tables=%d views=%d statements=%d) data(databases=%d tables=%d completed=%d rows=%d batches=%d restarted=%d checkpoint=%s watermark=%s:%d)",
			schemaSummary.Databases,
			schemaSummary.Tables,
			schemaSummary.Views,
			schemaSummary.Statements,
			dataSummary.Databases,
			dataSummary.Tables,
			dataSummary.Completed,
			dataSummary.RowsCopied,
			dataSummary.Batches,
			dataSummary.Restarted,
			dataSummary.CheckpointFile,
			dataSummary.WatermarkFile,
			dataSummary.WatermarkPos,
		)
		return writeResult(out, cfg, "migrate", "ok", message)
	})
}

func isMigrateCompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "incompatible_for_")
}

func parseMigrateOptions(args []string) (migrateOptions, error) {
	opts := migrateOptions{
		DestEmptyRequired: true,
		ChunkSize:         1000,
	}

	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.SchemaOnly, "schema-only", false, "migrate schema only")
	fs.BoolVar(&opts.DataOnly, "data-only", false, "migrate data only")
	fs.BoolVar(&opts.DestEmptyRequired, "dest-empty-required", true, "require empty destination before applying migration")
	fs.BoolVar(&opts.Force, "force", false, "force migration even when destination contains user objects")
	fs.IntVar(&opts.ChunkSize, "chunk-size", 1000, "rows per batch when migrating data")
	fs.BoolVar(&opts.Resume, "resume", false, "resume from checkpoint state in --state-dir")

	if err := fs.Parse(args); err != nil {
		return migrateOptions{}, err
	}
	if opts.ChunkSize < 1 {
		return migrateOptions{}, errors.New("chunk-size must be >= 1")
	}
	return opts, nil
}

func runMigrateDryRunSandbox(
	ctx context.Context,
	cfg config.RuntimeConfig,
	runSchema bool,
	runData bool,
	includeTables bool,
	includeViews bool,
	opts migrateOptions,
	out io.Writer,
) error {
	if runSchema && !includeTables && !includeViews {
		return errors.New("schema migration currently supports tables/views in --include-objects")
	}
	if runData && !includeTables {
		return errors.New("data migration requires tables in --include-objects")
	}

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

	runID := strconvBase36(time.Now().UTC().UnixNano())
	mapDatabase := func(sourceDBName string) string {
		return sandboxDatabaseName(runID, sourceDBName)
	}

	report := migrateDryRunSandboxResult{
		Command:       "migrate",
		Status:        "dry-run",
		DryRunMode:    "sandbox",
		CleanupStatus: "skipped",
		Timestamp:     time.Now().UTC(),
		Version:       version.Version,
	}

	sandboxCreated := make([]string, 0, 8)
	var validationErr error

	// Sandbox schema bootstrap is required for schema checks and for DML rollback validation.
	if runSchema || runData {
		schemaSummary, err := schema.ValidateSchemaInSandbox(ctx, sourceDB, destDB, schema.DryRunSandboxOptions{
			IncludeDatabases: cfg.Databases,
			ExcludeDatabases: cfg.ExcludeDatabases,
			IncludeTables:    includeTables,
			IncludeViews:     runSchema && includeViews,
			MapDatabase:      mapDatabase,
		})
		report.Validated += schemaSummary.Validated
		report.Failed += schemaSummary.Failed
		sandboxCreated = append(sandboxCreated, schemaSummary.CreatedDatabases...)
		if err != nil {
			validationErr = fmt.Errorf("schema dry-run validation failed: %w", err)
		}
	}

	if validationErr == nil && runData {
		dataSummary, err := data.ValidateBaselineDataDryRun(ctx, sourceDB, destDB, data.DryRunValidationOptions{
			IncludeDatabases: cfg.Databases,
			ExcludeDatabases: cfg.ExcludeDatabases,
			ChunkSize:        opts.ChunkSize,
			MapDatabase:      mapDatabase,
		})
		report.Validated += dataSummary.Validated
		report.Failed += dataSummary.Failed
		if err != nil {
			validationErr = fmt.Errorf("data dry-run validation failed: %w", err)
		}
	}

	cleanupErr := cleanupSandboxDatabases(ctx, destDB, sandboxCreated)
	if cleanupErr != nil {
		report.CleanupStatus = "failed"
	} else if len(sandboxCreated) > 0 {
		report.CleanupStatus = "ok"
	}

	if validationErr != nil {
		report.Message = validationErr.Error()
	}
	if cleanupErr != nil {
		if report.Message != "" {
			report.Message += "; "
		}
		report.Message += fmt.Sprintf("sandbox cleanup failed: %v", cleanupErr)
	}

	if err := writeMigrateDryRunSandboxReport(out, cfg, report); err != nil {
		return err
	}
	if validationErr != nil {
		if isMigrateCompatibilityError(validationErr) {
			return WithExitCode(ExitCodeDiff, validationErr)
		}
		return validationErr
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

func writeMigrateDryRunSandboxReport(out io.Writer, cfg config.RuntimeConfig, report migrateDryRunSandboxResult) error {
	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	if _, err := fmt.Fprintf(
		out,
		"[migrate] status=%s dry_run_mode=%s validated=%d failed=%d cleanup_status=%s\n",
		report.Status,
		report.DryRunMode,
		report.Validated,
		report.Failed,
		report.CleanupStatus,
	); err != nil {
		return err
	}
	if strings.TrimSpace(report.Message) != "" {
		if _, err := fmt.Fprintf(out, "[migrate] detail=%s\n", report.Message); err != nil {
			return err
		}
	}
	return nil
}

func cleanupSandboxDatabases(ctx context.Context, dest *sql.DB, names []string) error {
	if len(names) == 0 {
		return nil
	}
	unique := make(map[string]struct{}, len(names))
	for _, name := range names {
		normalized := strings.TrimSpace(name)
		if normalized == "" {
			continue
		}
		unique[normalized] = struct{}{}
	}
	toDrop := make([]string, 0, len(unique))
	for name := range unique {
		toDrop = append(toDrop, name)
	}
	sort.Strings(toDrop)
	var firstErr error
	for _, name := range toDrop {
		query := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(name))
		if _, err := dest.ExecContext(ctx, query); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func sandboxDatabaseName(runID string, sourceDatabase string) string {
	const maxLen = 64
	const prefix = "dbmigrate_dr_"
	normalized := normalizeIdentifier(sourceDatabase)
	if normalized == "" {
		normalized = "db"
	}
	name := fmt.Sprintf("%s%s_%s", prefix, runID, normalized)
	if len(name) <= maxLen {
		return name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(sourceDatabase))
	suffix := fmt.Sprintf("_%08x", hash.Sum32())
	maxBase := maxLen - len(prefix) - len(runID) - 1 - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(normalized) > maxBase {
		normalized = normalized[:maxBase]
	}
	return fmt.Sprintf("%s%s_%s%s", prefix, runID, normalized, suffix)
}

func normalizeIdentifier(in string) string {
	trimmed := strings.TrimSpace(strings.ToLower(in))
	if trimmed == "" {
		return ""
	}
	builder := strings.Builder{}
	builder.Grow(len(trimmed))
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			builder.WriteRune(r)
			continue
		}
		builder.WriteByte('_')
	}
	return strings.Trim(builder.String(), "_")
}

func strconvBase36(v int64) string {
	return strings.ToLower(strconv.FormatInt(v, 36))
}

func hasObject(objects []string, target string) bool {
	for _, object := range objects {
		if object == target {
			return true
		}
	}
	return false
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
