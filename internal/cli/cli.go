package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/esthergb/dbmigrate/internal/commands"
	"github.com/esthergb/dbmigrate/internal/config"
)

const (
	exitOK    = 0
	exitUsage = 1
	exitRun   = 3
)

// Run executes the dbmigrate command line.
func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer) int {
	registry := commands.Registry()
	if len(args) == 0 {
		writeHelp(stdout, registry)
		return exitOK
	}

	if args[0] == "-h" || args[0] == "--help" {
		writeHelp(stdout, registry)
		return exitOK
	}

	if args[0] == "version" {
		commands.WriteVersion(stdout)
		return exitOK
	}

	handler, ok := registry[args[0]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "unknown subcommand %q\n", args[0])
		writeHelp(stderr, registry)
		return exitUsage
	}

	var cfg config.RuntimeConfig
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	config.BindGlobalFlags(fs, &cfg)

	globalArgs, commandArgs, err := splitGlobalAndCommandArgs(args[1:])
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitUsage
	}
	if err := fs.Parse(globalArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	explicit := config.CollectSetFlags(fs)
	if cfg.ConfigFile != "" {
		fileCfg, err := config.LoadFileConfig(cfg.ConfigFile)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "invalid config file: %v\n", err)
			return exitUsage
		}
		config.MergeFileConfig(&cfg, fileCfg, explicit)
	}

	cfg.Finalize()
	if err := cfg.ValidateBasic(); err != nil {
		_, _ = fmt.Fprintf(stderr, "invalid configuration: %v\n", err)
		return exitUsage
	}

	if err := handler(ctx, cfg, commandArgs, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s failed: %v\n", args[0], err)
		if code, ok := commands.ResolveExitCode(err); ok {
			return code
		}
		return exitRun
	}
	return exitOK
}

func writeHelp(out io.Writer, registry map[string]commands.Handler) {
	_, _ = fmt.Fprintln(out, "dbmigrate - MySQL/MariaDB migration and replication tool")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Usage:")
	_, _ = fmt.Fprintln(out, "  dbmigrate <subcommand> [flags]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Subcommands:")

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		_, _ = fmt.Fprintf(out, "  %-10s %s\n", name, commands.Synopsis(name))
	}
	_, _ = fmt.Fprintln(out, "  version    print build version")
}

func splitGlobalAndCommandArgs(raw []string) ([]string, []string, error) {
	globalFlagsWithValue := map[string]struct{}{
		"source":            {},
		"dest":              {},
		"config":            {},
		"databases":         {},
		"exclude-databases": {},
		"include-objects":   {},
		"concurrency":       {},
		"tls-mode":          {},
		"ca-file":           {},
		"cert-file":         {},
		"key-file":          {},
		"state-dir":         {},
		"downgrade-profile": {},
		"dry-run-mode":      {},
	}
	globalBoolFlags := map[string]struct{}{
		"dry-run": {},
		"verbose": {},
		"json":    {},
	}

	global := make([]string, 0, len(raw))
	command := make([]string, 0, len(raw))

	for i := 0; i < len(raw); i++ {
		token := raw[i]
		if token == "--" {
			command = append(command, raw[i+1:]...)
			break
		}

		if !strings.HasPrefix(token, "--") {
			command = append(command, token)
			continue
		}

		name := strings.TrimPrefix(token, "--")
		hasValueInline := false
		if idx := strings.Index(name, "="); idx >= 0 {
			name = name[:idx]
			hasValueInline = true
		}

		if _, ok := globalBoolFlags[name]; ok {
			global = append(global, token)
			continue
		}
		if _, ok := globalFlagsWithValue[name]; ok {
			if !hasValueInline {
				if i+1 >= len(raw) || strings.HasPrefix(raw[i+1], "--") {
					return nil, nil, fmt.Errorf("missing value for global flag --%s", name)
				}
			}
			global = append(global, token)
			if !hasValueInline {
				global = append(global, raw[i+1])
				i++
			}
			continue
		}

		command = append(command, token)
	}

	return global, command, nil
}
