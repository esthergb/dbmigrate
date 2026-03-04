package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
		return err
	}

	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("verify requires both --source and --dest (or config file equivalents)")
	}
	if cfg.DryRun {
		return writeResult(
			out,
			cfg,
			"verify",
			fmt.Sprintf("dry-run: verify plan ready (verify_level=%s data_mode=%s sample_size=%d)", opts.VerifyLevel, opts.DataMode, opts.SampleSize),
		)
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

	includeTables := hasObject(cfg.IncludeObjects, "tables")
	includeViews := hasObject(cfg.IncludeObjects, "views")

	switch opts.VerifyLevel {
	case "schema":
		if !includeTables && !includeViews {
			return errors.New("schema verification requires tables/views in --include-objects")
		}

		summary, err := schemaVerify.Verify(ctx, sourceDB, destDB, schemaVerify.Options{
			IncludeDatabases: cfg.Databases,
			ExcludeDatabases: cfg.ExcludeDatabases,
			IncludeTables:    includeTables,
			IncludeViews:     includeViews,
		})
		if err != nil {
			return fmt.Errorf("schema verification failed: %w", err)
		}

		if err := writeSchemaVerifyResult(out, cfg, opts.VerifyLevel, summary); err != nil {
			return err
		}
		if len(summary.Diffs) > 0 {
			return fmt.Errorf(
				"schema differences detected: missing_in_destination=%d missing_in_source=%d definition_mismatches=%d",
				summary.MissingInDestination,
				summary.MissingInSource,
				summary.DefinitionMismatches,
			)
		}
		return nil
	case "data":
		if !includeTables {
			return errors.New("data verification requires tables in --include-objects")
		}

		switch opts.DataMode {
		case "count":
			summary, err := dataVerify.VerifyCount(ctx, sourceDB, destDB, dataVerify.Options{
				IncludeDatabases: cfg.Databases,
				ExcludeDatabases: cfg.ExcludeDatabases,
			})
			if err != nil {
				return fmt.Errorf("data verification failed: %w", err)
			}
			if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
				return err
			}
			if len(summary.Diffs) > 0 {
				return fmt.Errorf(
					"data differences detected: missing_in_destination=%d missing_in_source=%d count_mismatches=%d",
					summary.MissingInDestination,
					summary.MissingInSource,
					summary.CountMismatches,
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
				return fmt.Errorf("data verification failed: %w", err)
			}
			if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
				return err
			}
			if len(summary.Diffs) > 0 {
				return fmt.Errorf(
					"data differences detected: missing_in_destination=%d missing_in_source=%d hash_mismatches=%d",
					summary.MissingInDestination,
					summary.MissingInSource,
					summary.HashMismatches,
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
				return fmt.Errorf("data verification failed: %w", err)
			}
			if err := writeDataVerifyResult(out, cfg, opts.VerifyLevel, opts.DataMode, summary); err != nil {
				return err
			}
			if len(summary.Diffs) > 0 {
				return fmt.Errorf(
					"data differences detected: missing_in_destination=%d missing_in_source=%d hash_mismatches=%d",
					summary.MissingInDestination,
					summary.MissingInSource,
					summary.HashMismatches,
				)
			}
			return nil
		default:
			return fmt.Errorf("data-mode %q is not implemented yet; supported: count, hash, sample", opts.DataMode)
		}
	default:
		return fmt.Errorf("verify-level %q is not implemented", opts.VerifyLevel)
	}
}

func parseVerifyOptions(args []string) (verifyOptions, error) {
	opts := verifyOptions{
		VerifyLevel: "schema",
		DataMode:    "count",
		SampleSize:  1000,
	}

	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.VerifyLevel, "verify-level", "schema", "verification level (schema|data)")
	fs.StringVar(&opts.DataMode, "data-mode", "count", "data verification mode (count|hash|sample|full-hash)")
	fs.IntVar(&opts.SampleSize, "sample-size", 1000, "rows per table to hash in data-mode=sample")
	if err := fs.Parse(args); err != nil {
		return verifyOptions{}, err
	}
	if opts.SampleSize < 1 {
		return verifyOptions{}, errors.New("sample-size must be >= 1")
	}
	switch opts.VerifyLevel {
	case "schema", "data":
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
		"[verify] level=%s data_mode=%s status=%s databases=%d compared=%d missing_in_destination=%d missing_in_source=%d count_mismatches=%d hash_mismatches=%d\n",
		level,
		dataMode,
		status,
		summary.Databases,
		summary.TablesCompared,
		summary.MissingInDestination,
		summary.MissingInSource,
		summary.CountMismatches,
		summary.HashMismatches,
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
	}
	return nil
}
