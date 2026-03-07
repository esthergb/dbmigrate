package commands

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/esthergb/dbmigrate/internal/compat"
)

type replicationBoundaryPrecheckReport struct {
	Name                               string           `json:"name"`
	SourceVersion                      string           `json:"source_version"`
	DestVersion                        string           `json:"dest_version"`
	CrossEngine                        bool             `json:"cross_engine"`
	SourceLogBinKnown                  bool             `json:"source_log_bin_known"`
	SourceLogBin                       bool             `json:"source_log_bin,omitempty"`
	SourceBinlogFormat                 string           `json:"source_binlog_format,omitempty"`
	SourceGTIDMode                     string           `json:"source_gtid_mode,omitempty"`
	SourceGTIDPosition                 string           `json:"source_gtid_position,omitempty"`
	DestGTIDMode                       string           `json:"dest_gtid_mode,omitempty"`
	DestGTIDPosition                   string           `json:"dest_gtid_position,omitempty"`
	SourceBinlogRowValueOptions        string           `json:"source_binlog_row_value_options,omitempty"`
	SourceBinlogTransactionCompression string           `json:"source_binlog_transaction_compression,omitempty"`
	Findings                           []compat.Finding `json:"findings,omitempty"`
}

func runReplicationBoundaryPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
) (replicationBoundaryPrecheckReport, error) {
	report := replicationBoundaryPrecheckReport{
		Name:          "replication-boundary",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
		CrossEngine:   sourceInstance.Engine != destInstance.Engine,
	}
	if source == nil || dest == nil {
		return report, fmt.Errorf("source and destination connections are required")
	}
	if !report.CrossEngine {
		report.Findings = []compat.Finding{{
			Code:     "replication_boundary_same_engine_inventory_skipped",
			Severity: "info",
			Message:  "Cross-engine GTID/file-position boundary inventory is not required for same-engine lanes.",
			Proposal: "Use normal same-engine replication evidence and keep start-position handling aligned with the selected lane.",
		}}
		return report, nil
	}

	if value, known, err := queryOptionalBooleanVariable(ctx, source, "@@GLOBAL.log_bin"); err != nil {
		report.Findings = append(report.Findings, compat.Finding{
			Code:     "source_log_bin_inventory_unavailable",
			Severity: "warn",
			Message:  fmt.Sprintf("Unable to read source @@GLOBAL.log_bin during replication boundary precheck: %v.", err),
			Proposal: "Verify binlog availability manually before assuming incremental continuity is available.",
		})
	} else if known {
		report.SourceLogBinKnown = true
		report.SourceLogBin = value
	}

	report.SourceBinlogFormat = queryOptionalStringVariableOrEmpty(ctx, source, "@@GLOBAL.binlog_format")
	report.SourceGTIDMode = detectGTIDMode(ctx, source, sourceInstance)
	report.SourceGTIDPosition = detectGTIDPosition(ctx, source, sourceInstance)
	report.DestGTIDMode = detectGTIDMode(ctx, dest, destInstance)
	report.DestGTIDPosition = detectGTIDPosition(ctx, dest, destInstance)
	if sourceInstance.Engine == compat.EngineMySQL && destInstance.Engine == compat.EngineMariaDB {
		report.SourceBinlogRowValueOptions = queryOptionalStringVariableOrEmpty(ctx, source, "@@GLOBAL.binlog_row_value_options")
		report.SourceBinlogTransactionCompression = queryOptionalStringVariableOrEmpty(ctx, source, "@@GLOBAL.binlog_transaction_compression")
	}

	report.Findings = buildReplicationBoundaryFindings(report, sourceInstance, destInstance)
	return report, nil
}

func detectGTIDMode(ctx context.Context, db *sql.DB, instance compat.Instance) string {
	var candidates []string
	switch instance.Engine {
	case compat.EngineMySQL:
		candidates = []string{"@@GLOBAL.gtid_mode"}
	case compat.EngineMariaDB:
		candidates = []string{"@@GLOBAL.gtid_strict_mode", "@@GLOBAL.gtid_domain_id"}
	default:
		candidates = []string{"@@GLOBAL.gtid_mode", "@@GLOBAL.gtid_strict_mode"}
	}
	for _, candidate := range candidates {
		if value, ok := queryOptionalStringVariable(ctx, db, candidate); ok {
			return value
		}
	}
	return ""
}

func detectGTIDPosition(ctx context.Context, db *sql.DB, instance compat.Instance) string {
	var candidates []string
	switch instance.Engine {
	case compat.EngineMySQL:
		candidates = []string{"@@GLOBAL.gtid_executed"}
	case compat.EngineMariaDB:
		candidates = []string{"@@GLOBAL.gtid_current_pos", "@@GLOBAL.gtid_binlog_pos", "@@GLOBAL.gtid_slave_pos"}
	default:
		candidates = []string{"@@GLOBAL.gtid_executed", "@@GLOBAL.gtid_current_pos", "@@GLOBAL.gtid_binlog_pos"}
	}
	for _, candidate := range candidates {
		if value, ok := queryOptionalStringVariable(ctx, db, candidate); ok {
			return value
		}
	}
	return ""
}

func queryOptionalStringVariableOrEmpty(ctx context.Context, db *sql.DB, expression string) string {
	value, ok := queryOptionalStringVariable(ctx, db, expression)
	if !ok {
		return ""
	}
	return value
}

func queryOptionalStringVariable(ctx context.Context, db *sql.DB, expression string) (string, bool) {
	var raw sql.NullString
	query := fmt.Sprintf("SELECT %s", expression)
	if err := db.QueryRowContext(ctx, query).Scan(&raw); err != nil {
		return "", false
	}
	return strings.TrimSpace(raw.String), true
}

func buildReplicationBoundaryFindings(report replicationBoundaryPrecheckReport, sourceInstance compat.Instance, destInstance compat.Instance) []compat.Finding {
	findings := make([]compat.Finding, 0, 8)
	findings = append(findings, compat.Finding{
		Code:     "cross_engine_gtid_boundary_detected",
		Severity: "warn",
		Message:  fmt.Sprintf("Cross-engine replication boundary detected: %s -> %s must not rely on GTID auto-position in v1.", sourceInstance.Engine, destInstance.Engine),
		Proposal: "Use file/position continuity for v1 incremental cutovers. Treat GTID state as evidence only, not as the resume contract.",
	})
	if report.SourceGTIDMode != "" || report.SourceGTIDPosition != "" || report.DestGTIDMode != "" || report.DestGTIDPosition != "" {
		findings = append(findings, compat.Finding{
			Code:     "cross_engine_gtid_state_inventory_recorded",
			Severity: "warn",
			Message:  fmt.Sprintf("Recorded source GTID state (%s / %s) and destination GTID state (%s / %s) across a non-portable engine boundary.", emptyAsUnknown(report.SourceGTIDMode), emptyAsUnknown(report.SourceGTIDPosition), emptyAsUnknown(report.DestGTIDMode), emptyAsUnknown(report.DestGTIDPosition)),
			Proposal: "Do not attempt GTID surgery or auto-position shortcuts across this boundary. Capture file/position handoff evidence and reseed using documented engine-specific workflows only.",
		})
	}
	if report.SourceLogBinKnown && !report.SourceLogBin {
		findings = append(findings, compat.Finding{
			Code:     "cross_engine_replication_source_log_bin_disabled",
			Severity: "warn",
			Message:  "Source binary logging is disabled, so incremental file/position continuity is unavailable.",
			Proposal: "Enable source binary logging and collect a fresh baseline watermark before planning incremental continuity.",
		})
	}
	if report.SourceBinlogFormat != "" && !strings.EqualFold(report.SourceBinlogFormat, "ROW") {
		findings = append(findings, compat.Finding{
			Code:     "cross_engine_replication_non_row_binlog_format",
			Severity: "warn",
			Message:  fmt.Sprintf("Source binlog_format is %q; cross-engine continuity assumes row-based logging for v1 safety.", report.SourceBinlogFormat),
			Proposal: "Switch the source to ROW binlog format before collecting continuity evidence for a cross-engine replay lane.",
		})
	}
	if sourceInstance.Engine == compat.EngineMySQL && destInstance.Engine == compat.EngineMariaDB {
		if strings.TrimSpace(report.SourceBinlogRowValueOptions) != "" {
			findings = append(findings, compat.Finding{
				Code:     "mysql_to_mariadb_binlog_row_value_options_set",
				Severity: "warn",
				Message:  fmt.Sprintf("Source binlog_row_value_options=%q on a MySQL -> MariaDB boundary may emit row events MariaDB cannot consume safely.", report.SourceBinlogRowValueOptions),
				Proposal: "Clear binlog_row_value_options, rotate to fresh binlogs, and collect new file/position evidence before any incremental cross-engine rehearsal.",
			})
		}
		if strings.EqualFold(strings.TrimSpace(report.SourceBinlogTransactionCompression), "on") || strings.TrimSpace(report.SourceBinlogTransactionCompression) == "1" {
			findings = append(findings, compat.Finding{
				Code:     "mysql_to_mariadb_binlog_transaction_compression_enabled",
				Severity: "warn",
				Message:  "Source binlog_transaction_compression is enabled on a MySQL -> MariaDB boundary.",
				Proposal: "Disable binlog transaction compression, rotate to fresh binlogs, and re-collect the replication handoff evidence before cross-engine incremental replay.",
			})
		}
	}
	if len(findings) == 0 {
		findings = append(findings, compat.Finding{
			Code:     "cross_engine_gtid_boundary_inventory_clean",
			Severity: "info",
			Message:  "Cross-engine GTID/file-position inventory did not reveal obvious continuity boundary warnings.",
			Proposal: "Keep using file/position for v1 and attach the boundary inventory to the cutover report.",
		})
	}
	return findings
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
