package config

import (
	"errors"
	"flag"
	"fmt"
	"strings"
)

var validTLSModes = map[string]struct{}{
	"disabled":  {},
	"preferred": {},
	"required":  {},
}

// RuntimeConfig holds global options shared by all subcommands.
type RuntimeConfig struct {
	Source           string
	Dest             string
	Databases        []string
	ExcludeDatabases []string
	IncludeObjects   []string
	Concurrency      int
	DryRun           bool
	Verbose          bool
	JSON             bool
	TLSMode          string
	CAFile           string
	CertFile         string
	KeyFile          string
	StateDir         string

	databasesRaw        string
	excludeDatabasesRaw string
	includeObjectsRaw   string
}

// BindGlobalFlags binds global CLI flags to the target config.
func BindGlobalFlags(fs *flag.FlagSet, cfg *RuntimeConfig) {
	fs.StringVar(&cfg.Source, "source", "", "source DSN")
	fs.StringVar(&cfg.Dest, "dest", "", "destination DSN")
	fs.StringVar(&cfg.databasesRaw, "databases", "", "comma-separated databases")
	fs.StringVar(&cfg.excludeDatabasesRaw, "exclude-databases", "information_schema,performance_schema,sys,mysql", "comma-separated excluded databases")
	fs.StringVar(&cfg.includeObjectsRaw, "include-objects", "tables,views,routines,triggers,events", "comma-separated object types")
	fs.IntVar(&cfg.Concurrency, "concurrency", 4, "worker concurrency")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "plan actions without applying changes")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "verbose logs")
	fs.BoolVar(&cfg.JSON, "json", false, "JSON output mode")
	fs.StringVar(&cfg.TLSMode, "tls-mode", "preferred", "TLS mode: disabled, preferred, required")
	fs.StringVar(&cfg.CAFile, "ca-file", "", "TLS CA file")
	fs.StringVar(&cfg.CertFile, "cert-file", "", "TLS client cert file")
	fs.StringVar(&cfg.KeyFile, "key-file", "", "TLS client key file")
	fs.StringVar(&cfg.StateDir, "state-dir", "./state", "checkpoint and metadata directory")
}

// Finalize normalizes derived fields after flag parsing.
func (c *RuntimeConfig) Finalize() {
	c.Databases = csvToList(c.databasesRaw)
	c.ExcludeDatabases = csvToList(c.excludeDatabasesRaw)
	c.IncludeObjects = csvToList(c.includeObjectsRaw)

	if len(c.Databases) == 0 {
		c.Databases = nil
	}
	if len(c.ExcludeDatabases) == 0 {
		c.ExcludeDatabases = []string{"information_schema", "performance_schema", "sys", "mysql"}
	}
	if len(c.IncludeObjects) == 0 {
		c.IncludeObjects = []string{"tables", "views", "routines", "triggers", "events"}
	}
}

// ValidateBasic validates global runtime options without touching network resources.
func (c RuntimeConfig) ValidateBasic() error {
	if c.Concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if _, ok := validTLSModes[c.TLSMode]; !ok {
		return fmt.Errorf("invalid tls-mode %q", c.TLSMode)
	}
	if c.Source != "" && !strings.Contains(c.Source, "://") {
		return errors.New("source must be a DSN URI")
	}
	if c.Dest != "" && !strings.Contains(c.Dest, "://") {
		return errors.New("dest must be a DSN URI")
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
