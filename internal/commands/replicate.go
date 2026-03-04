package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

func runReplicate(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	return writeResult(out, cfg, "replicate", "phase 1 skeleton ready; incremental replication will be implemented in phase 5")
}
