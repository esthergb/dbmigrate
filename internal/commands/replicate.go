package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/replicate/binlog"
)

type replicateOptions struct {
	ApplyDDL  string
	Resume    bool
	StartFile string
	StartPos  uint
}

func runReplicate(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error {
	opts, err := parseReplicateOptions(args)
	if err != nil {
		return err
	}
	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("replicate requires both --source and --dest (or config file equivalents)")
	}
	if cfg.DryRun {
		return writeResult(
			out,
			cfg,
			"replicate",
			fmt.Sprintf(
				"dry-run: replicate plan ready (resume=%v apply_ddl=%s start_file=%s start_pos=%d)",
				opts.Resume,
				opts.ApplyDDL,
				opts.StartFile,
				opts.StartPos,
			),
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

	summary, err := binlog.Run(ctx, sourceDB, destDB, cfg.StateDir, binlog.Options{
		ApplyDDL:  opts.ApplyDDL,
		Resume:    opts.Resume,
		StartFile: opts.StartFile,
		StartPos:  uint32(opts.StartPos),
		SourceDSN: cfg.Source,
	})
	if err != nil {
		return fmt.Errorf("replicate run failed: %w", err)
	}

	return writeResult(
		out,
		cfg,
		"replicate",
		fmt.Sprintf(
			"replication checkpoint updated: source(log_bin=%v format=%s row_image=%s) start=%s:%d source_end=%s:%d applied_end=%s:%d applied_events=%d apply_ddl=%s checkpoint=%s",
			summary.SourceLogBin,
			summary.SourceFormat,
			summary.SourceRowImage,
			summary.StartFile,
			summary.StartPos,
			summary.SourceEndFile,
			summary.SourceEndPos,
			summary.EndFile,
			summary.EndPos,
			summary.AppliedEvents,
			summary.ApplyDDL,
			summary.CheckpointFile,
		),
	)
}

func parseReplicateOptions(args []string) (replicateOptions, error) {
	opts := replicateOptions{
		ApplyDDL: "warn",
		Resume:   true,
		StartPos: 4,
	}

	fs := flag.NewFlagSet("replicate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.ApplyDDL, "apply-ddl", "warn", "DDL policy during replication (ignore|apply|warn)")
	fs.BoolVar(&opts.Resume, "resume", true, "resume from replication checkpoint in --state-dir")
	fs.StringVar(&opts.StartFile, "start-file", "", "start binlog file when no checkpoint exists")
	fs.UintVar(&opts.StartPos, "start-pos", 4, "start binlog position when no checkpoint exists")

	if err := fs.Parse(args); err != nil {
		return replicateOptions{}, err
	}
	switch opts.ApplyDDL {
	case "ignore", "apply", "warn":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --apply-ddl value %q (expected ignore, apply, or warn)", opts.ApplyDDL)
	}
	if opts.StartPos < 4 {
		return replicateOptions{}, errors.New("start-pos must be >= 4")
	}
	return opts, nil
}
