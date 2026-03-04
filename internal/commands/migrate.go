package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/schema"
)

type migrateOptions struct {
	SchemaOnly        bool
	DataOnly          bool
	DestEmptyRequired bool
	Force             bool
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
	if !opts.SchemaOnly && !opts.DataOnly {
		return errors.New("full schema+data migration is not implemented yet; use --schema-only for now")
	}
	if opts.DataOnly {
		return errors.New("data-only migration is not implemented yet")
	}

	if cfg.DryRun {
		return writeResult(out, cfg, "migrate", "dry-run: schema baseline migration plan is ready")
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

	copyOptions := schema.CopyOptions{
		IncludeDatabases:  cfg.Databases,
		ExcludeDatabases:  cfg.ExcludeDatabases,
		IncludeTables:     hasObject(cfg.IncludeObjects, "tables"),
		IncludeViews:      hasObject(cfg.IncludeObjects, "views"),
		DestEmptyRequired: opts.DestEmptyRequired && !opts.Force,
	}
	if !copyOptions.IncludeTables && !copyOptions.IncludeViews {
		return errors.New("phase 3 schema migration currently supports only tables/views in --include-objects")
	}

	summary, err := schema.CopySchema(ctx, sourceDB, destDB, copyOptions)
	if err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}

	message := fmt.Sprintf(
		"schema migration completed: databases=%d tables=%d views=%d statements=%d",
		summary.Databases,
		summary.Tables,
		summary.Views,
		summary.Statements,
	)
	return writeResult(out, cfg, "migrate", message)
}

func parseMigrateOptions(args []string) (migrateOptions, error) {
	opts := migrateOptions{DestEmptyRequired: true}

	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.SchemaOnly, "schema-only", false, "migrate schema only")
	fs.BoolVar(&opts.DataOnly, "data-only", false, "migrate data only")
	fs.BoolVar(&opts.DestEmptyRequired, "dest-empty-required", true, "require empty destination before applying migration")
	fs.BoolVar(&opts.Force, "force", false, "force migration even when destination contains user objects")

	if err := fs.Parse(args); err != nil {
		return migrateOptions{}, err
	}
	return opts, nil
}

func hasObject(objects []string, target string) bool {
	for _, object := range objects {
		if object == target {
			return true
		}
	}
	return false
}
