package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/version"
)

type commandResult struct {
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

func writeResult(out io.Writer, cfg config.RuntimeConfig, command, message string) error {
	result := commandResult{
		Command:   command,
		Status:    "phase1-scaffold",
		Message:   message,
		Timestamp: time.Now().UTC(),
		Version:   version.Version,
	}

	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	_, err := fmt.Fprintf(out, "[%s] %s\n", command, message)
	return err
}

// WriteVersion prints the binary version information.
func WriteVersion(out io.Writer) {
	fmt.Fprintf(out, "dbmigrate version=%s commit=%s build_date=%s\n", version.Version, version.Commit, version.BuildDate)
}
