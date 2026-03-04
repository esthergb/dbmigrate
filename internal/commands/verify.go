package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

func runVerify(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	return writeResult(out, cfg, "verify", "phase 1 skeleton ready; schema/data verification engine will be implemented in phase 4")
}
