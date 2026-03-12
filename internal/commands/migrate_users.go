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
	"github.com/esthergb/dbmigrate/internal/users"
	"github.com/esthergb/dbmigrate/internal/version"
)

type migrateUsersOptions struct {
	Scope      string
	DryRun     bool
	SkipLocked bool
}

type migrateUsersResult struct {
	Command string           `json:"command"`
	Status  string           `json:"status"`
	Summary users.UserSummary `json:"summary"`
	Timestamp time.Time      `json:"timestamp"`
	Version string           `json:"version"`
}

func runMigrateUsers(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error {
	opts, err := parseMigrateUsersOptions(args)
	if err != nil {
		return err
	}
	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("migrate-users requires both --source and --dest")
	}

	if cfg.DryRun {
		return writeResult(out, cfg, "migrate-users", "dry-run",
			fmt.Sprintf("dry-run: migrate-users plan ready (scope=%s skip_locked=%v)", opts.Scope, opts.SkipLocked))
	}

	sourceDB, err := db.OpenAndPingWithTLS(ctx, cfg.Source, tlsOptionsFromRuntime(cfg))
	if err != nil {
		return fmt.Errorf("connect source: %w", err)
	}
	defer func() { _ = sourceDB.Close() }()

	destDB, err := db.OpenAndPingWithTLS(ctx, cfg.Dest, tlsOptionsFromRuntime(cfg))
	if err != nil {
		return fmt.Errorf("connect destination: %w", err)
	}
	defer func() { _ = destDB.Close() }()

	scope := users.ScopeBusiness
	if opts.Scope == "all" {
		scope = users.ScopeAll
	}

	summary, err := users.CopyUsers(ctx, sourceDB, destDB, users.CopyOptions{
		Scope:      scope,
		DryRun:     opts.DryRun,
		SkipLocked: opts.SkipLocked,
		Log:        cfg.Log,
	})
	if err != nil {
		return fmt.Errorf("user migration failed: %w", err)
	}

	return writeMigrateUsersResult(out, cfg, summary)
}

func parseMigrateUsersOptions(args []string) (migrateUsersOptions, error) {
	opts := migrateUsersOptions{
		Scope: "business",
	}
	fs := flag.NewFlagSet("migrate-users", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.Scope, "scope", "business", "user scope: business (non-system) or all")
	fs.BoolVar(&opts.SkipLocked, "skip-locked", false, "skip locked accounts instead of failing")
	if err := fs.Parse(args); err != nil {
		return migrateUsersOptions{}, err
	}
	switch opts.Scope {
	case "business", "all":
	default:
		return migrateUsersOptions{}, fmt.Errorf("invalid scope %q: must be business or all", opts.Scope)
	}
	return opts, nil
}

func writeMigrateUsersResult(out io.Writer, cfg config.RuntimeConfig, summary users.UserSummary) error {
	status := "ok"
	if summary.DryRun {
		status = "dry-run"
	}

	if cfg.JSON {
		payload := migrateUsersResult{
			Command:   "migrate-users",
			Status:    status,
			Summary:   summary,
			Timestamp: time.Now().UTC(),
			Version:   version.Version,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	_, err := fmt.Fprintf(out,
		"[migrate-users] status=%s users_found=%d users_skipped=%d users_copied=%d grants_copied=%d dry_run=%v\n",
		status,
		summary.UsersFound,
		summary.UsersSkipped,
		summary.UsersCopied,
		summary.GrantsCopied,
		summary.DryRun,
	)
	return err
}
