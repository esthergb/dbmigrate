package hybrid

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/esthergb/dbmigrate/internal/dblog"
	"github.com/esthergb/dbmigrate/internal/replicate/binlog"
	"github.com/esthergb/dbmigrate/internal/replicate/cdc"
)

// TableMode defines the replication mode for a single table.
type TableMode string

const (
	TableModeBinlog          TableMode = "binlog"
	TableModeCaptureTriggers TableMode = "capture-triggers"
)

// TableRouting maps schema.table to its replication mode.
type TableRouting map[string]TableMode

// Options controls hybrid replication behavior.
type Options struct {
	ApplyDDL        string
	ConflictPolicy  string
	ConflictValues  string
	MaxEvents       uint64
	MaxLagSeconds   uint64
	SourceServerID  uint32
	Idempotent      bool
	Resume          bool
	StartFile       string
	StartPos        uint32
	GTIDSet         string
	Routing         TableRouting
	CDCDatabases    []string
	BinlogDatabases []string
	SourceDSN       string
	SourceTLSMode   string
	SourceCAFile    string
	SourceCertFile  string
	SourceKeyFile   string
	RateLimit       int
	Log             *dblog.Logger
}

// Summary reports hybrid replication results.
type Summary struct {
	BinlogSummary binlog.Summary
	CDCSummary    cdc.Summary
	Mode          string
}

// ValidateRouting checks that a TableRouting definition has no conflicts:
//   - every entry must have the format "schema.table"
//   - no table may appear with two different modes
//   - no table may be assigned to both CDC and binlog databases implicitly
func ValidateRouting(routing TableRouting) error {
	for key := range routing {
		parts := strings.SplitN(key, ".", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("invalid routing key %q: expected \"schema.table\"", key)
		}
	}
	return nil
}

// tableSetForMode returns the lower-cased "schema.table" keys routed to the given mode.
func tableSetForMode(routing TableRouting, mode TableMode) map[string]struct{} {
	out := make(map[string]struct{}, len(routing))
	for key, m := range routing {
		if m == mode {
			out[strings.ToLower(key)] = struct{}{}
		}
	}
	return out
}

// Run executes hybrid replication: CDC for configured tables, binlog for the rest.
func Run(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("hybrid: source and destination connections are required")
	}

	if err := ValidateRouting(opts.Routing); err != nil {
		return Summary{}, fmt.Errorf("hybrid: invalid routing: %w", err)
	}

	cdcTableSet := tableSetForMode(opts.Routing, TableModeCaptureTriggers)
	binlogTableSet := tableSetForMode(opts.Routing, TableModeBinlog)

	cdcStateDir := filepath.Join(stateDir, "hybrid-cdc")
	binlogStateDir := filepath.Join(stateDir, "hybrid-binlog")

	var cdcSummary cdc.Summary
	var binlogSummary binlog.Summary

	if len(opts.CDCDatabases) > 0 {
		cs, err := runCDCPhase(ctx, source, dest, cdcStateDir, opts, cdcTableSet, binlogTableSet)
		if err != nil {
			return Summary{}, fmt.Errorf("hybrid CDC phase: %w", err)
		}
		cdcSummary = cs
	}

	if len(opts.BinlogDatabases) > 0 || len(opts.CDCDatabases) == 0 {
		bs, err := runBinlogPhase(ctx, source, dest, binlogStateDir, opts, cdcTableSet, binlogTableSet)
		if err != nil {
			return Summary{}, fmt.Errorf("hybrid binlog phase: %w", err)
		}
		binlogSummary = bs
	}

	return Summary{
		BinlogSummary: binlogSummary,
		CDCSummary:    cdcSummary,
		Mode:          "hybrid",
	}, nil
}

func runCDCPhase(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options, cdcTables map[string]struct{}, binlogTables map[string]struct{}) (cdc.Summary, error) {
	cdcOpts := cdc.Options{
		ApplyDDL:         opts.ApplyDDL,
		ConflictPolicy:   opts.ConflictPolicy,
		ConflictValues:   opts.ConflictValues,
		MaxEvents:        opts.MaxEvents,
		IncludeDatabases: opts.CDCDatabases,
		Resume:           opts.Resume,
		RateLimit:        opts.RateLimit,
		Log:              opts.Log,
	}
	if len(cdcTables) > 0 {
		cdcOpts.IncludeTables = cdcTables
	}
	if len(binlogTables) > 0 {
		cdcOpts.ExcludeTables = binlogTables
	}
	return cdcRunFn(ctx, source, dest, stateDir, cdcOpts)
}

func runBinlogPhase(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts Options, cdcTables map[string]struct{}, binlogTables map[string]struct{}) (binlog.Summary, error) {
	binlogOpts := binlog.Options{
		ApplyDDL:       opts.ApplyDDL,
		ConflictPolicy: opts.ConflictPolicy,
		ConflictValues: opts.ConflictValues,
		MaxEvents:      opts.MaxEvents,
		MaxLagSeconds:  opts.MaxLagSeconds,
		SourceServerID: opts.SourceServerID,
		Idempotent:     opts.Idempotent,
		Resume:         opts.Resume,
		StartFile:      opts.StartFile,
		StartPos:       opts.StartPos,
		GTIDSet:        opts.GTIDSet,
		SourceDSN:      opts.SourceDSN,
		SourceTLSMode:  opts.SourceTLSMode,
		SourceCAFile:   opts.SourceCAFile,
		SourceCertFile: opts.SourceCertFile,
		SourceKeyFile:  opts.SourceKeyFile,
		RateLimit:      opts.RateLimit,
		Log:            opts.Log,
	}
	if len(cdcTables) > 0 {
		binlogOpts.ExcludeTables = cdcTables
	}
	return binlogRunFn(ctx, source, dest, stateDir, binlogOpts)
}

var (
	cdcRunFn    = cdc.Run
	binlogRunFn = binlog.Run
)

// ParseTableRouting parses a comma-separated list of "schema.table=mode" pairs.
// Example: "app.orders=capture-triggers,app.events=binlog"
func ParseTableRouting(raw string) (TableRouting, error) {
	routing := make(TableRouting)
	if strings.TrimSpace(raw) == "" {
		return routing, nil
	}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid table routing entry %q (expected schema.table=mode)", entry)
		}
		key := strings.TrimSpace(parts[0])
		mode := TableMode(strings.TrimSpace(parts[1]))
		switch mode {
		case TableModeBinlog, TableModeCaptureTriggers:
		default:
			return nil, fmt.Errorf("invalid replication mode %q for %q (expected binlog or capture-triggers)", mode, key)
		}
		routing[key] = mode
	}
	return routing, nil
}

// DatabasesForMode returns the set of databases that have at least one table
// routed to the given mode, or all databases if routing is empty.
func DatabasesForMode(routing TableRouting, mode TableMode, allDatabases []string) []string {
	if len(routing) == 0 {
		return allDatabases
	}
	set := make(map[string]struct{})
	for key, m := range routing {
		if m == mode {
			parts := strings.SplitN(key, ".", 2)
			if len(parts) == 2 {
				set[parts[0]] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for db := range set {
		out = append(out, db)
	}
	return out
}
