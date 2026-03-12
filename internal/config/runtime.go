package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/db"
	"github.com/esthergb/dbmigrate/internal/dblog"
)

var validTLSModes = map[string]struct{}{
	"disabled":  {},
	"preferred": {},
	"required":  {},
}

var validDowngradeProfiles = map[string]struct{}{
	"strict-lts":     {},
	"same-major":     {},
	"adjacent-minor": {},
	"max-compat":     {},
}

var validDryRunModes = map[string]struct{}{
	"plan":    {},
	"sandbox": {},
}

// RuntimeConfig holds global options shared by all subcommands.
type RuntimeConfig struct {
	Source           string
	Dest             string
	ConfigFile       string
	Databases        []string
	ExcludeDatabases []string
	IncludeObjects   []string
	Concurrency      int
	DryRun           bool
	DryRunMode       string
	Verbose          bool
	JSON             bool
	TLSMode          string
	CAFile           string
	CertFile         string
	KeyFile          string
	OperationTimeout time.Duration
	StateDir         string
	DowngradeProfile string
	Log              *dblog.Logger

	databasesRaw        string
	excludeDatabasesRaw string
	includeObjectsRaw   string
}

// BindGlobalFlags binds global CLI flags to the target config.
func BindGlobalFlags(fs *flag.FlagSet, cfg *RuntimeConfig) {
	fs.StringVar(&cfg.Source, "source", "", "source DSN")
	fs.StringVar(&cfg.Dest, "dest", "", "destination DSN")
	fs.StringVar(&cfg.ConfigFile, "config", "", "optional path to YAML/JSON config file")
	fs.StringVar(&cfg.databasesRaw, "databases", "", "comma-separated databases")
	fs.StringVar(&cfg.excludeDatabasesRaw, "exclude-databases", "information_schema,performance_schema,sys,mysql", "comma-separated excluded databases")
	fs.StringVar(&cfg.includeObjectsRaw, "include-objects", "tables,views", "comma-separated object types")
	fs.IntVar(&cfg.Concurrency, "concurrency", 4, "worker concurrency")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "plan actions without applying changes")
	fs.StringVar(&cfg.DryRunMode, "dry-run-mode", "plan", "dry-run behavior: plan, sandbox")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "verbose logs")
	fs.BoolVar(&cfg.JSON, "json", false, "JSON output mode")
	fs.StringVar(&cfg.TLSMode, "tls-mode", "required", "TLS mode: disabled, preferred, required")
	fs.StringVar(&cfg.CAFile, "ca-file", "", "TLS CA file")
	fs.StringVar(&cfg.CertFile, "cert-file", "", "TLS client cert file")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "TLS client key file")
	fs.DurationVar(&cfg.OperationTimeout, "operation-timeout", 0, "global operation timeout (0 disables deadline)")
	fs.StringVar(&cfg.StateDir, "state-dir", "./state", "checkpoint and metadata directory")
	fs.StringVar(&cfg.DowngradeProfile, "downgrade-profile", "strict-lts", "downgrade compatibility profile: strict-lts, same-major, adjacent-minor, max-compat")
}

// Finalize normalizes derived fields after flag parsing.
func (c *RuntimeConfig) Finalize() {
	if c.databasesRaw != "" {
		c.Databases = csvToList(c.databasesRaw)
	}
	if c.excludeDatabasesRaw != "" {
		c.ExcludeDatabases = csvToList(c.excludeDatabasesRaw)
	}
	if c.includeObjectsRaw != "" {
		c.IncludeObjects = csvToList(c.includeObjectsRaw)
	}

	if len(c.Databases) == 0 {
		c.Databases = nil
	}
	if len(c.ExcludeDatabases) == 0 {
		c.ExcludeDatabases = []string{"information_schema", "performance_schema", "sys", "mysql"}
	}
	if len(c.IncludeObjects) == 0 {
		c.IncludeObjects = []string{"tables", "views"}
	}
}

// ValidateBasic validates global runtime options without touching network resources.
func (c RuntimeConfig) ValidateBasic() error {
	if c.Concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if c.OperationTimeout < 0 {
		return errors.New("operation-timeout must be >= 0")
	}
	if _, ok := validTLSModes[c.TLSMode]; !ok {
		return fmt.Errorf("invalid tls-mode %q", c.TLSMode)
	}
	if _, ok := validDowngradeProfiles[c.DowngradeProfile]; !ok {
		return fmt.Errorf("invalid downgrade-profile %q", c.DowngradeProfile)
	}
	if _, ok := validDryRunModes[c.DryRunMode]; !ok {
		return fmt.Errorf("invalid dry-run-mode %q", c.DryRunMode)
	}
	if err := validateRuntimeDSN(c.Source, "source"); err != nil {
		return err
	}
	if err := validateRuntimeDSN(c.Dest, "dest"); err != nil {
		return err
	}
	return nil
}

func validateRuntimeDSN(raw string, field string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if _, err := db.NormalizeDSN(raw); err != nil {
		return fmt.Errorf("%s has invalid DSN format: %w", field, err)
	}
	return nil
}

func csvToList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// CollectSetFlags returns the flag names explicitly set in CLI arguments.
func CollectSetFlags(fs *flag.FlagSet) map[string]struct{} {
	out := map[string]struct{}{}
	fs.Visit(func(f *flag.Flag) {
		out[f.Name] = struct{}{}
	})
	return out
}
