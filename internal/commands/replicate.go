package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/replicate/binlog"
)

type replicateOptions struct {
	ReplicationMode  string
	StartFrom        string
	GTIDSet          string
	MaxEvents        uint64
	MaxLagSeconds    uint64
	SourceServerID   uint64
	Idempotent       bool
	ConflictValues   string
	ApplyDDL         string
	ConflictPolicy   string
	EnableTriggerCDC bool
	TeardownCDC      bool
	Resume           bool
	StartFile        string
	StartPos         uint
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
			"dry-run",
			fmt.Sprintf(
				"dry-run: replicate plan ready (replication_mode=%s start_from=%s max_events=%d max_lag_seconds=%d source_server_id=%d conflict_values=%s enable_trigger_cdc=%v teardown_cdc=%v resume=%v apply_ddl=%s conflict_policy=%s start_file=%s start_pos=%d)",
				opts.ReplicationMode,
				opts.StartFrom,
				opts.MaxEvents,
				opts.MaxLagSeconds,
				opts.SourceServerID,
				opts.ConflictValues,
				opts.EnableTriggerCDC,
				opts.TeardownCDC,
				opts.Resume,
				opts.ApplyDDL,
				opts.ConflictPolicy,
				opts.StartFile,
				opts.StartPos,
			),
		)
	}
	if opts.EnableTriggerCDC || opts.TeardownCDC {
		return WithExitCode(
			ExitCodeDiff,
			errors.New("trigger CDC mode is not implemented yet; --enable-trigger-cdc/--teardown-cdc are reserved for capture-triggers/hybrid replication"),
		)
	}
	if opts.ReplicationMode != "binlog" {
		return WithExitCode(
			ExitCodeDiff,
			fmt.Errorf(
				"replication-mode %q is not implemented yet; currently supported mode is binlog (planned: capture-triggers, hybrid)",
				opts.ReplicationMode,
			),
		)
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

		summary, err := binlog.Run(ctx, sourceDB, destDB, cfg.StateDir, binlog.Options{
			ApplyDDL:       opts.ApplyDDL,
			ConflictPolicy: opts.ConflictPolicy,
			MaxEvents:      opts.MaxEvents,
			MaxLagSeconds:  opts.MaxLagSeconds,
			SourceServerID: uint32(opts.SourceServerID),
			Idempotent:     opts.Idempotent,
			ConflictValues: opts.ConflictValues,
			Resume:         opts.Resume,
			StartFile:      opts.StartFile,
			StartPos:       uint32(opts.StartPos),
			GTIDSet:        opts.GTIDSet,
			SourceDSN:      cfg.Source,
			SourceTLSMode:  cfg.TLSMode,
			SourceCAFile:   cfg.CAFile,
			SourceCertFile: cfg.CertFile,
			SourceKeyFile:  cfg.KeyFile,
			RateLimit:      cfg.RateLimit,
			Log:            cfg.Log,
		})
		if err != nil {
			return fmt.Errorf("replicate run failed: %w", err)
		}

		return writeResult(
			out,
			cfg,
			"replicate",
			"ok",
			fmt.Sprintf(
				"replication checkpoint updated: source(log_bin=%v format=%s row_image=%s) start=%s:%d source_end=%s:%d applied_end=%s:%d applied_events=%d tx_shape(seen=%d applied=%d max_events=%d risk=%s signals=%s) replication_mode=%s start_from=%s max_events=%d max_lag_seconds=%d source_server_id=%d conflict_values=%s apply_ddl=%s conflict_policy=%s checkpoint=%s",
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
				summary.Shape.TransactionsSeen,
				summary.Shape.TransactionsApplied,
				summary.Shape.MaxTransactionEvents,
				summary.Shape.RiskLevel,
				strings.Join(summary.Shape.RiskSignals, ","),
				opts.ReplicationMode,
				opts.StartFrom,
				opts.MaxEvents,
				opts.MaxLagSeconds,
				opts.SourceServerID,
				opts.ConflictValues,
				summary.ApplyDDL,
				summary.ConflictPolicy,
				summary.CheckpointFile,
			),
		)
	})
}

func parseReplicateOptions(args []string) (replicateOptions, error) {
	opts := replicateOptions{
		ReplicationMode: "binlog",
		StartFrom:       "auto",
		MaxEvents:       0,
		MaxLagSeconds:   0,
		SourceServerID:  0,
		Idempotent:      false,
		ConflictValues:  "redacted",
		ApplyDDL:        "warn",
		ConflictPolicy:  "fail",
		Resume:          true,
		StartPos:        4,
	}

	fs := flag.NewFlagSet("replicate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.ReplicationMode, "replication-mode", "binlog", "replication mode (binlog|capture-triggers|hybrid)")
	fs.StringVar(&opts.StartFrom, "start-from", "auto", "replication start reference (auto|binlog-file:pos|gtid)")
	fs.Uint64Var(&opts.MaxEvents, "max-events", 0, "max apply events per run (0 means no explicit limit)")
	fs.Uint64Var(&opts.MaxLagSeconds, "max-lag-seconds", 0, "max allowed lag in seconds before apply (planned)")
	fs.Uint64Var(&opts.SourceServerID, "source-server-id", 0, "source replication client server_id override (1..4294967295, 0 uses derived default)")
	fs.BoolVar(&opts.Idempotent, "idempotent", false, "enforce idempotent-safe conflict policy for replay runs")
	fs.StringVar(&opts.ConflictValues, "conflict-values", "redacted", "conflict report value mode (redacted|plain)")
	fs.StringVar(&opts.ApplyDDL, "apply-ddl", "warn", "DDL policy during replication (ignore|apply|warn)")
	fs.StringVar(&opts.ConflictPolicy, "conflict-policy", "fail", "conflict policy (fail|source-wins|dest-wins)")
	fs.BoolVar(&opts.EnableTriggerCDC, "enable-trigger-cdc", false, "enable trigger-based CDC setup (planned for capture-triggers/hybrid modes)")
	fs.BoolVar(&opts.TeardownCDC, "teardown-cdc", false, "remove trigger-based CDC objects (planned for capture-triggers/hybrid modes)")
	fs.BoolVar(&opts.Resume, "resume", true, "resume from replication checkpoint in --state-dir")
	fs.StringVar(&opts.StartFile, "start-file", "", "start binlog file when no checkpoint exists")
	fs.UintVar(&opts.StartPos, "start-pos", 4, "start binlog position when no checkpoint exists")
	fs.StringVar(&opts.GTIDSet, "gtid-set", "", "GTID set to start from when --start-from=gtid (MySQL: uuid:interval, MariaDB: domain-server-seq)")

	if err := fs.Parse(args); err != nil {
		return replicateOptions{}, err
	}
	switch opts.ReplicationMode {
	case "binlog", "capture-triggers", "hybrid":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --replication-mode value %q (expected binlog, capture-triggers, or hybrid)", opts.ReplicationMode)
	}
	switch opts.StartFrom {
	case "auto", "binlog-file:pos", "gtid":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --start-from value %q (expected auto, binlog-file:pos, or gtid)", opts.StartFrom)
	}
	if opts.StartFrom == "binlog-file:pos" {
		if opts.Resume {
			return replicateOptions{}, errors.New("--resume must be false when --start-from=binlog-file:pos")
		}
		if opts.StartFile == "" {
			return replicateOptions{}, errors.New("--start-file is required when --start-from=binlog-file:pos")
		}
	}
	if opts.StartFrom == "gtid" {
		if strings.TrimSpace(opts.GTIDSet) == "" {
			return replicateOptions{}, errors.New("--gtid-set is required when --start-from=gtid")
		}
	}
	if strings.TrimSpace(opts.GTIDSet) != "" && opts.StartFrom != "gtid" {
		return replicateOptions{}, errors.New("--gtid-set requires --start-from=gtid")
	}
	if err := validateBinlogFileName(opts.StartFile); err != nil {
		return replicateOptions{}, err
	}
	switch opts.ApplyDDL {
	case "ignore", "apply", "warn":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --apply-ddl value %q (expected ignore, apply, or warn)", opts.ApplyDDL)
	}
	switch opts.ConflictPolicy {
	case "fail", "source-wins", "dest-wins":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --conflict-policy value %q (expected fail, source-wins, or dest-wins)", opts.ConflictPolicy)
	}
	if opts.Idempotent {
		return replicateOptions{}, errors.New("--idempotent is reserved for v2 and is unsupported in v1")
	}
	switch opts.ConflictValues {
	case "redacted", "plain":
		// valid
	default:
		return replicateOptions{}, fmt.Errorf("invalid --conflict-values value %q (expected redacted or plain)", opts.ConflictValues)
	}
	if opts.StartPos < 4 {
		return replicateOptions{}, errors.New("start-pos must be >= 4")
	}
	if opts.SourceServerID > math.MaxUint32 {
		return replicateOptions{}, errors.New("source-server-id must be <= 4294967295")
	}
	return opts, nil
}

func validateBinlogFileName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil
	}
	if len(trimmed) > 255 {
		return errors.New("start-file must be <= 255 characters")
	}
	if strings.Contains(trimmed, "..") || strings.ContainsAny(trimmed, `/\`) {
		return errors.New("start-file must be a bare binlog filename without path traversal")
	}
	for _, r := range trimmed {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '.', '_', '-':
			continue
		default:
			return fmt.Errorf("start-file contains invalid character %q", r)
		}
	}
	return nil
}
