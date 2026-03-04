package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

func runReport(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	return writeResult(out, cfg, "report", "phase 1 skeleton ready; structured reporting will be expanded in phase 7")
}
