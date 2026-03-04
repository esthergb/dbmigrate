package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

func runPlan(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	return writeResult(out, cfg, "plan", "phase 1 skeleton ready; compatibility analyzers will be implemented in phase 2")
}
