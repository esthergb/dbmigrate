package commands

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/db"
)

func runPlan(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	if cfg.Source == "" || cfg.Dest == "" {
		return errors.New("plan requires both --source and --dest (or config file equivalents)")
	}

	sourceDSN, err := db.NormalizeDSN(cfg.Source)
	if err != nil {
		return fmt.Errorf("invalid source DSN: %w", err)
	}
	destDSN, err := db.NormalizeDSN(cfg.Dest)
	if err != nil {
		return fmt.Errorf("invalid dest DSN: %w", err)
	}

	_ = sourceDSN
	_ = destDSN

	return writeResult(out, cfg, "plan", "phase 2 foundation: configuration precedence and DSN validation are active")
}
