package commands

import (
	"context"
	"io"

	"github.com/esthergb/dbmigrate/internal/config"
)

// Handler executes a single subcommand.
type Handler func(ctx context.Context, cfg config.RuntimeConfig, args []string, out io.Writer) error

// Registry returns the available command handlers.
func Registry() map[string]Handler {
	return map[string]Handler{
		"plan":          runPlan,
		"migrate":       runMigrate,
		"migrate-users": runMigrateUsers,
		"replicate":     runReplicate,
		"verify":        runVerify,
		"report":        runReport,
	}
}

// Synopsis returns a concise description for a subcommand.
func Synopsis(name string) string {
	synopsis := map[string]string{
		"plan":          "compute compatibility plan and warnings",
		"migrate":       "run baseline schema/data migration",
		"migrate-users": "migrate user accounts and grants",
		"replicate":     "apply incremental changes from checkpoints",
		"verify":        "compare schema and data consistency",
		"report":        "emit machine-readable migration report",
	}
	return synopsis[name]
}
