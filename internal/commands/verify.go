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
	schemaVerify "github.com/esthergb/dbmigrate/internal/verify/schema"
	"github.com/esthergb/dbmigrate/internal/version"
)

type verifyOptions struct {
	VerifyLevel string
}

type verifyResult struct {
	Command     string               `json:"command"`
	Status      string               `json:"status"`
	VerifyLevel string               `json:"verify_level"`
	Summary     schemaVerify.Summary `json:"summary"`
	Timestamp   time.Time            `json:"timestamp"`
	Version     string               `json:"version"`
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
		return writeResult(out, cfg, "verify", fmt.Sprintf("dry-run: verify plan ready (verify_level=%s)", opts.VerifyLevel))
	}
	if opts.VerifyLevel != "schema" {
		return fmt.Errorf("verify-level %q is not implemented yet; supported: schema", opts.VerifyLevel)
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

	if err := writeVerifyResult(out, cfg, opts.VerifyLevel, summary); err != nil {
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
}

func parseVerifyOptions(args []string) (verifyOptions, error) {
	opts := verifyOptions{
		VerifyLevel: "schema",
	}

	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.VerifyLevel, "verify-level", "schema", "verification level (schema|data)")
	if err := fs.Parse(args); err != nil {
		return verifyOptions{}, err
	}
	switch opts.VerifyLevel {
	case "schema", "data":
		return opts, nil
	default:
		return verifyOptions{}, fmt.Errorf("invalid verify-level %q", opts.VerifyLevel)
	}
}

func writeVerifyResult(out io.Writer, cfg config.RuntimeConfig, level string, summary schemaVerify.Summary) error {
	status := "ok"
	if len(summary.Diffs) > 0 {
		status = "diff"
	}

	if cfg.JSON {
		payload := verifyResult{
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
