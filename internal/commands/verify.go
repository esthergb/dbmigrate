package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
	dataVerify "github.com/esthergb/dbmigrate/internal/verify/data"
	schemaVerify "github.com/esthergb/dbmigrate/internal/verify/schema"
	"github.com/esthergb/dbmigrate/internal/version"
)

type verifyOptions struct {
	VerifyLevel string
	DataMode    string
	SampleSize  int
}

type verifySchemaGranularResult struct {
	Command     string                       `json:"command"`
	Status      string                       `json:"status"`
	VerifyLevel string                       `json:"verify_level"`
	Summary     schemaVerify.GranularSummary `json:"summary"`
	Timestamp   time.Time                    `json:"timestamp"`
	Version     string                       `json:"version"`
}

type verifySchemaResult struct {
	Command     string               `json:"command"`
	Status      string               `json:"status"`
	VerifyLevel string               `json:"verify_level"`
	Summary     schemaVerify.Summary `json:"summary"`
	Timestamp   time.Time            `json:"timestamp"`
	Version     string               `json:"version"`
}

type verifyDataResult struct {
	Command     string             `json:"command"`
	Status      string             `json:"status"`
	VerifyLevel string             `json:"verify_level"`
	DataMode    string             `json:"data_mode"`
	Summary     dataVerify.Summary `json:"summary"`
	Timestamp   time.Time          `json:"timestamp"`
	Version     string             `json:"version"`
}

func runVerify(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error {
	opts, err := parseVerifyOptions(args)
	if err != nil {
		return WithExitCode(ExitCodeVerifyFailed, err)
	}

	if cfg.Source == "" || cfg.Dest == "" {
		return WithExitCode(ExitCodeVerifyFailed, errors.New("verify requires both --source and --dest (or config file equivalents)"))
	}
	if unsupported := unsupportedV2IncludeObjects(cfg.IncludeObjects); len(unsupported) > 0 {
		return WithExitCode(ExitCodeDiff, fmt.Errorf("--include-objects contains unknown types (%s); supported: tables, views, routines, triggers, events", strings.Join(unsupported, ",")))
	}
	if cfg.DryRun {
		return writeResult(
			out,
			cfg,
			"verify",
			"dry-run",
			fmt.Sprintf("dry-run: verify plan ready (verify_level=%s data_mode=%s sample_size=%d)", opts.VerifyLevel, opts.DataMode, opts.SampleSize),
		)
	}

	return withStateDirLock(cfg, func() error {
		sourceDB, err := db.OpenAndPingWithTLS(ctx, cfg.Source, tlsOptionsFromRuntime(cfg))
		if err != nil {
			return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("connect source: %w", err))
		}
		defer func() {
			_ = sourceDB.Close()
		}()

		destDB, err := db.OpenAndPingWithTLS(ctx, cfg.Dest, tlsOptionsFromRuntime(cfg))
		if err != nil {
			return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("connect destination: %w", err))
		}
		defer func() {
			_ = destDB.Close()
		}()

		includeTables := hasObject(cfg.IncludeObjects, "tables")
		includeViews := hasObject(cfg.IncludeObjects, "views")
		includeRoutines := hasObject(cfg.IncludeObjects, "routines")
		includeTriggers := hasObject(cfg.IncludeObjects, "triggers")
		includeEvents := hasObject(cfg.IncludeObjects, "events")

		switch opts.VerifyLevel {
		case "schema":
			if !includeTables && !includeViews && !includeRoutines && !includeTriggers && !includeEvents {
				return WithExitCode(ExitCodeVerifyFailed, errors.New("schema verification requires at least one object type in --include-objects"))
			}

			summary, err := schemaVerify.Verify(ctx, sourceDB, destDB, schemaVerify.Options{
				IncludeDatabases: cfg.Databases,
				ExcludeDatabases: cfg.ExcludeDatabases,
				IncludeTables:    includeTables,
				IncludeViews:     includeViews,
				IncludeRoutines:  includeRoutines,
				IncludeTriggers:  includeTriggers,
				IncludeEvents:    includeEvents,
			})
			if err != nil {
				return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("schema verification failed: %w", err))
			}

			if err := writeSchemaVerifyResult(out, cfg, opts.VerifyLevel, summary); err != nil {
				return WithExitCode(ExitCodeVerifyFailed, err)
			}
			if len(summary.Diffs) > 0 {
				return WithExitCode(
					ExitCodeDiff,
					fmt.Errorf(
						"schema differences detected: missing_in_destination=%d missing_in_source=%d definition_mismatches=%d",
						summary.MissingInDestination,
						summary.MissingInSource,
						summary.DefinitionMismatches,
					),
				)
			}
			return nil
		case "schema-granular":
			if !includeTables {
				return WithExitCode(ExitCodeVerifyFailed, errors.New("schema-granular verification requires tables in --include-objects"))
			}

			granularSummary, err := schemaVerify.VerifyGranular(ctx, sourceDB, destDB, schemaVerify.Options{
				IncludeDatabases: cfg.Databases,
				ExcludeDatabases: cfg.ExcludeDatabases,
				IncludeTables:    true,
			})
			if err != nil {
				return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("schema-granular verification failed: %w", err))
			}

			if err := writeGranularVerifyResult(out, cfg, granularSummary); err != nil {
				return WithExitCode(ExitCodeVerifyFailed, err)
			}
			if len(granularSummary.Diffs) > 0 {
				return WithExitCode(
					ExitCodeDiff,
					fmt.Errorf(
						"granular schema differences detected: column_diffs=%d index_diffs=%d fk_diffs=%d partition_diffs=%d",
						granularSummary.ColumnDiffs,
						granularSummary.IndexDiffs,
						granularSummary.FKDiffs,
						granularSummary.PartitionDiffs,
					),
				)
			}
			return nil
		case "data":
			if !includeTables {
				return WithExitCode(ExitCodeVerifyFailed, errors.New("data verification requires tables in --include-objects"))
			}

			switch opts.DataMode {
			case "count":
				summary, err := dataVerify.VerifyCount(ctx, sourceDB, destDB, dataVerify.Options{
					IncludeDatabases: cfg.Databases,
					ExcludeDatabases: cfg.ExcludeDatabases,
				})
				if err != nil {
					return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("data verification failed: %w", err))
				}
				if err := persistVerifyDataArtifact(cfg.StateDir, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if len(summary.Diffs) > 0 {
					return WithExitCode(
						ExitCodeDiff,
						fmt.Errorf(
							"data differences detected: missing_in_destination=%d missing_in_source=%d count_mismatches=%d",
							summary.MissingInDestination,
							summary.MissingInSource,
							summary.CountMismatches,
						),
					)
				}
				return nil
			case "hash":
				summary, err := dataVerify.VerifyHash(ctx, sourceDB, destDB, dataVerify.Options{
					IncludeDatabases: cfg.Databases,
					ExcludeDatabases: cfg.ExcludeDatabases,
					SampleSize:       opts.SampleSize,
				})
				if err != nil {
					return WithExitCode(verifyDataExitCode(err), fmt.Errorf("data verification failed: %w", err))
				}
				if err := persistVerifyDataArtifact(cfg.StateDir, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if len(summary.Diffs) > 0 {
					return WithExitCode(
						ExitCodeDiff,
						fmt.Errorf(
							"data differences detected: missing_in_destination=%d missing_in_source=%d hash_mismatches=%d",
							summary.MissingInDestination,
							summary.MissingInSource,
							summary.HashMismatches,
						),
					)
				}
				return nil
			case "sample":
				summary, err := dataVerify.VerifySample(ctx, sourceDB, destDB, dataVerify.Options{
					IncludeDatabases: cfg.Databases,
					ExcludeDatabases: cfg.ExcludeDatabases,
					SampleSize:       opts.SampleSize,
				})
				if err != nil {
					return WithExitCode(verifyDataExitCode(err), fmt.Errorf("data verification failed: %w", err))
				}
				if err := persistVerifyDataArtifact(cfg.StateDir, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if len(summary.Diffs) > 0 {
					return WithExitCode(
						ExitCodeDiff,
						fmt.Errorf(
							"data differences detected: missing_in_destination=%d missing_in_source=%d hash_mismatches=%d",
							summary.MissingInDestination,
							summary.MissingInSource,
							summary.HashMismatches,
						),
					)
				}
				return nil
			case "full-hash":
				summary, err := dataVerify.VerifyFullHash(ctx, sourceDB, destDB, dataVerify.Options{
					IncludeDatabases: cfg.Databases,
					ExcludeDatabases: cfg.ExcludeDatabases,
					SampleSize:       opts.SampleSize,
				})
				if err != nil {
					return WithExitCode(verifyDataExitCode(err), fmt.Errorf("data verification failed: %w", err))
				}
				if err := persistVerifyDataArtifact(cfg.StateDir, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
					return WithExitCode(ExitCodeVerifyFailed, err)
				}
				if len(summary.Diffs) > 0 {
					return WithExitCode(
						ExitCodeDiff,
						fmt.Errorf(
							"data differences detected: missing_in_destination=%d missing_in_source=%d hash_mismatches=%d",
							summary.MissingInDestination,
							summary.MissingInSource,
							summary.HashMismatches,
						),
					)
				}
				return nil
			default:
				return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("data-mode %q is not implemented yet; supported: count, hash, sample, full-hash", opts.DataMode))
			}
		default:
			return WithExitCode(ExitCodeVerifyFailed, fmt.Errorf("verify-level %q is not implemented", opts.VerifyLevel))
		}
	})
}

func verifyDataExitCode(err error) int {
	if strings.Contains(err.Error(), "incompatible_for_v1_deterministic_hash") || strings.Contains(err.Error(), "incompatible_for_v1_deterministic_sample") {
		return ExitCodeDiff
	}
	return ExitCodeVerifyFailed
}

func parseVerifyOptions(args []string) (verifyOptions, error) {
	opts := verifyOptions{
		VerifyLevel: "schema",
		DataMode:    "count",
		SampleSize:  1000,
	}

	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.VerifyLevel, "verify-level", "schema", "verification level (schema|schema-granular|data)")
	fs.StringVar(&opts.DataMode, "data-mode", "count", "data verification mode (count|hash|sample|full-hash)")
	fs.IntVar(&opts.SampleSize, "sample-size", 1000, "rows per table to hash in data-mode=sample")
	if err := fs.Parse(args); err != nil {
		return verifyOptions{}, err
	}
	if opts.SampleSize < 1 {
		return verifyOptions{}, errors.New("sample-size must be >= 1")
	}
	switch opts.VerifyLevel {
	case "schema", "schema-granular", "data":
		// valid
	default:
		return verifyOptions{}, fmt.Errorf("invalid verify-level %q", opts.VerifyLevel)
	}
	switch opts.DataMode {
	case "count", "hash", "sample", "full-hash":
		return opts, nil
	default:
		return verifyOptions{}, fmt.Errorf("invalid data-mode %q", opts.DataMode)
	}
}

func writeSchemaVerifyResult(out io.Writer, cfg config.RuntimeConfig, level string, summary schemaVerify.Summary) error {
	status := "ok"
	if len(summary.Diffs) > 0 {
		status = "diff"
	}

	if cfg.JSON {
		payload := verifySchemaResult{
			Command:     "verify",
			Status:      status,
			VerifyLevel: level,
			Summary:     summary,
			Timestamp:   time.Now().UTC(),
			Version:     version.Version,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[verify] level=%s status=%s databases=%d compared=%d missing_in_destination=%d missing_in_source=%d definition_mismatches=%d\n",
		level,
		status,
		summary.Databases,
		summary.ObjectsCompared,
		summary.MissingInDestination,
		summary.MissingInSource,
		summary.DefinitionMismatches,
	); err != nil {
		return err
	}

	for _, diff := range summary.Diffs {
		if _, err := fmt.Fprintf(out, "[verify] diff kind=%s database=%s object=%s:%s\n", diff.Kind, diff.Database, diff.ObjectType, diff.ObjectName); err != nil {
			return err
		}
	}
	return nil
}

func writeGranularVerifyResult(out io.Writer, cfg config.RuntimeConfig, summary schemaVerify.GranularSummary) error {
	status := "ok"
	if len(summary.Diffs) > 0 {
		status = "diff"
	}

	if cfg.JSON {
		payload := verifySchemaGranularResult{
			Command:     "verify",
			Status:      status,
			VerifyLevel: "schema-granular",
			Summary:     summary,
			Timestamp:   time.Now().UTC(),
			Version:     version.Version,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[verify] level=schema-granular status=%s databases=%d tables_compared=%d column_diffs=%d index_diffs=%d fk_diffs=%d partition_diffs=%d\n",
		status,
		summary.Databases,
		summary.TablesCompared,
		summary.ColumnDiffs,
		summary.IndexDiffs,
		summary.FKDiffs,
		summary.PartitionDiffs,
	); err != nil {
		return err
	}
	for _, d := range summary.Diffs {
		if _, err := fmt.Fprintf(out, "[verify] diff kind=%s database=%s table=%s object=%s\n", d.Kind, d.Database, d.Table, d.ObjectName); err != nil {
			return err
		}
	}
	return nil
}

func writeDataVerifyResult(out io.Writer, cfg config.RuntimeConfig, level string, dataMode string, summary dataVerify.Summary) error {
	status := "ok"
	if len(summary.Diffs) > 0 {
		status = "diff"
	}

	if cfg.JSON {
		payload := verifyDataResult{
			Command:     "verify",
			Status:      status,
			VerifyLevel: level,
			DataMode:    dataMode,
			Summary:     summary,
			Timestamp:   time.Now().UTC(),
			Version:     version.Version,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[verify] level=%s data_mode=%s status=%s databases=%d compared=%d missing_in_destination=%d missing_in_source=%d count_mismatches=%d hash_mismatches=%d noise_risk_mismatches=%d risk_tables=%d canonical(row_order_independent=%v session_time_zone=%s json=%v sample_full_scan=%v)\n",
		level,
		dataMode,
		status,
		summary.Databases,
		summary.TablesCompared,
		summary.MissingInDestination,
		summary.MissingInSource,
		summary.CountMismatches,
		summary.HashMismatches,
		summary.NoiseRiskMismatches,
		summary.RepresentationRiskTables,
		summary.Canonicalization.RowOrderIndependent,
		summary.Canonicalization.SessionTimeZone,
		summary.Canonicalization.JSONNormalized,
		summary.Canonicalization.SampleFullScan,
	); err != nil {
		return err
	}

	for _, diff := range summary.Diffs {
		if _, err := fmt.Fprintf(
			out,
			"[verify] diff kind=%s database=%s table=%s source_count=%d dest_count=%d source_hash=%s dest_hash=%s\n",
			diff.Kind,
			diff.Database,
			diff.Table,
			diff.SourceCount,
			diff.DestCount,
			diff.SourceHash,
			diff.DestHash,
		); err != nil {
			return err
		}
		if diff.NoiseRisk != "" {
			if _, err := fmt.Fprintf(out, "[verify] diff_note database=%s table=%s noise_risk=%s notes=%s\n", diff.Database, diff.Table, diff.NoiseRisk, strings.Join(diff.Notes, " | ")); err != nil {
				return err
			}
		}
	}
	for _, risk := range summary.TableRisks {
		if _, err := fmt.Fprintf(
			out,
			"[verify] table_risk database=%s table=%s approx_numeric=%d temporal=%d json=%d collation_sensitive=%d notes=%s\n",
			risk.Database,
			risk.Table,
			risk.ApproximateNumericColumns,
			risk.TemporalColumns,
			risk.JSONColumns,
			risk.CollationSensitiveColumns,
			strings.Join(risk.Notes, " | "),
		); err != nil {
			return err
		}
	}
	return nil
}
