package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/data"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/schema"
)

type migrateOptions struct {
	SchemaOnly        bool
	DataOnly          bool
	DestEmptyRequired bool
	Force             bool
	ChunkSize         int
	Resume            bool
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

	if cfg.DryRun {
		message := fmt.Sprintf(
			"dry-run: migrate plan ready (schema=%v data=%v chunk_size=%d resume=%v)",
			runSchema,
			runData,
			opts.ChunkSize,
			opts.Resume,
		)
		return writeResult(out, cfg, "migrate", "dry-run", message)
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

	schemaSummary := schema.CopySummary{}
	if runSchema {
		schemaOptions := schema.CopyOptions{
			IncludeDatabases:  cfg.Databases,
			ExcludeDatabases:  cfg.ExcludeDatabases,
			IncludeTables:     hasObject(cfg.IncludeObjects, "tables"),
			IncludeViews:      hasObject(cfg.IncludeObjects, "views"),
			DestEmptyRequired: opts.DestEmptyRequired && !opts.Force,
		}
		if !schemaOptions.IncludeTables && !schemaOptions.IncludeViews {
			return errors.New("schema migration currently supports tables/views in --include-objects")
		}

		schemaSummary, err = schema.CopySchema(ctx, sourceDB, destDB, schemaOptions)
		if err != nil {
			return fmt.Errorf("schema migration failed: %w", err)
		}
	}

	dataSummary := data.CopySummary{}
	if runData {
		if !hasObject(cfg.IncludeObjects, "tables") {
			return errors.New("data migration requires tables in --include-objects")
		}
		dataOptions := data.CopyOptions{
			IncludeDatabases: cfg.Databases,
			ExcludeDatabases: cfg.ExcludeDatabases,
			ChunkSize:        opts.ChunkSize,
			Resume:           opts.Resume,
			RequireEmptyDest: runData && !runSchema && opts.DestEmptyRequired && !opts.Force,
		}

		dataSummary, err = data.CopyBaselineData(ctx, sourceDB, destDB, cfg.StateDir, dataOptions)
		if err != nil {
			return fmt.Errorf("data migration failed: %w", err)
		}
	}

	message := fmt.Sprintf(
		"migration completed: schema(databases=%d tables=%d views=%d statements=%d) data(databases=%d tables=%d completed=%d rows=%d batches=%d restarted=%d checkpoint=%s)",
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
	)
	return writeResult(out, cfg, "migrate", "ok", message)
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

func hasObject(objects []string, target string) bool {
	for _, object := range objects {
		if object == target {
			return true
		}
	}
	return false
}
