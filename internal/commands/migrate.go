package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

func runMigrate(_ context.Context, cfg config.RuntimeConfig, _ []string, out io.Writer) error {
	return writeResult(out, cfg, "migrate", "phase 1 skeleton ready; baseline schema/data migration will be implemented in phase 3")
}
