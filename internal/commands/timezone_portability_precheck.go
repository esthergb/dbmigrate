package commands

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/esthergb/dbmigrate/internal/compat"
)

type timezonePortabilityPrecheckReport struct {
	Name                    string               `json:"name"`
	SourceVersion           string               `json:"source_version"`
	DestVersion             string               `json:"dest_version"`
	SourceSystemTimeZone    string               `json:"source_system_time_zone,omitempty"`
	SourceGlobalTimeZone    string               `json:"source_global_time_zone,omitempty"`
	SourceSessionTimeZone   string               `json:"source_session_time_zone,omitempty"`
	DestSystemTimeZone      string               `json:"dest_system_time_zone,omitempty"`
	DestGlobalTimeZone      string               `json:"dest_global_time_zone,omitempty"`
	DestSessionTimeZone     string               `json:"dest_session_time_zone,omitempty"`
	TemporalTableCount      int                  `json:"temporal_table_count"`
	MixedTemporalTableCount int                  `json:"mixed_temporal_table_count"`
	Tables                  []temporalTableIssue `json:"tables,omitempty"`
	Findings                []compat.Finding     `json:"findings,omitempty"`
}

type temporalTableIssue struct {
	Database       string `json:"database"`
	Table          string `json:"table"`
	TimestampCount int    `json:"timestamp_count"`
	DatetimeCount  int    `json:"datetime_count"`
	Proposal       string `json:"proposal"`
}

func runTimezonePortabilityPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	includeDatabases []string,
	excludeDatabases []string,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
) (timezonePortabilityPrecheckReport, error) {
	report := timezonePortabilityPrecheckReport{
		Name:          "timezone-portability",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
	}
	if source == nil || dest == nil {
		return report, fmt.Errorf("source and destination connections are required")
	}
	report.SourceSystemTimeZone = queryOptionalStringVariableOrEmpty(ctx, source, "@@system_time_zone")
	report.SourceGlobalTimeZone = queryOptionalStringVariableOrEmpty(ctx, source, "@@GLOBAL.time_zone")
	report.SourceSessionTimeZone = queryOptionalStringVariableOrEmpty(ctx, source, "@@SESSION.time_zone")
	report.DestSystemTimeZone = queryOptionalStringVariableOrEmpty(ctx, dest, "@@system_time_zone")
	report.DestGlobalTimeZone = queryOptionalStringVariableOrEmpty(ctx, dest, "@@GLOBAL.time_zone")
	report.DestSessionTimeZone = queryOptionalStringVariableOrEmpty(ctx, dest, "@@SESSION.time_zone")

	databases, err := listSelectableDatabases(ctx, source, includeDatabases, excludeDatabases)
	if err != nil {
		return report, err
	}
	if len(databases) == 0 {
		report.Findings = []compat.Finding{{
			Code:     "timezone_portability_no_selected_databases",
			Severity: "info",
			Message:  "No user databases selected for temporal/time-zone inventory.",
			Proposal: "Use --databases to narrow scope or rerun without filters to inspect all user schemas.",
		}}
		return report, nil
	}

	report.Tables, err = queryTemporalTableIssues(ctx, source, databases)
	if err != nil {
		return report, err
	}
	sortTemporalTableIssues(report.Tables)
	report.TemporalTableCount = len(report.Tables)
	for _, item := range report.Tables {
		if item.TimestampCount > 0 && item.DatetimeCount > 0 {
			report.MixedTemporalTableCount++
		}
	}
	report.Findings = buildTimezonePortabilityFindings(report)
	return report, nil
}

func queryTemporalTableIssues(ctx context.Context, source *sql.DB, databases []string) ([]temporalTableIssue, error) {
	placeholders, args := sqlPlaceholders(databases)
	rows, err := source.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME,
		       SUM(CASE WHEN DATA_TYPE = 'timestamp' THEN 1 ELSE 0 END) AS timestamp_count,
		       SUM(CASE WHEN DATA_TYPE = 'datetime' THEN 1 ELSE 0 END) AS datetime_count
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		  AND DATA_TYPE IN ('timestamp', 'datetime')
		GROUP BY TABLE_SCHEMA, TABLE_NAME
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query temporal columns for timezone portability: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]temporalTableIssue, 0, 8)
	for rows.Next() {
		var item temporalTableIssue
		if err := rows.Scan(&item.Database, &item.Table, &item.TimestampCount, &item.DatetimeCount); err != nil {
			return nil, err
		}
		item.Proposal = fmt.Sprintf("Review temporal semantics for %s.%s and document whether TIMESTAMP or DATETIME behavior is relied on during cutover.", item.Database, item.Table)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildTimezonePortabilityFindings(report timezonePortabilityPrecheckReport) []compat.Finding {
	findings := make([]compat.Finding, 0, 4+len(report.Tables))
	findings = append(findings, compat.Finding{
		Code:     "timezone_inventory_recorded",
		Severity: "info",
		Message:  fmt.Sprintf("Recorded time-zone inventory: source(system=%s global=%s session=%s) destination(system=%s global=%s session=%s).", emptyAsUnknown(report.SourceSystemTimeZone), emptyAsUnknown(report.SourceGlobalTimeZone), emptyAsUnknown(report.SourceSessionTimeZone), emptyAsUnknown(report.DestSystemTimeZone), emptyAsUnknown(report.DestGlobalTimeZone), emptyAsUnknown(report.DestSessionTimeZone)),
		Proposal: "Keep the time-zone inventory with the plan artifact so cutover decisions are tied to real server settings.",
	})
	if normalizeTimeZoneSetting(report.SourceSystemTimeZone, report.SourceGlobalTimeZone) != normalizeTimeZoneSetting(report.DestSystemTimeZone, report.DestGlobalTimeZone) {
		findings = append(findings, compat.Finding{
			Code:     "timezone_environment_drift_detected",
			Severity: "warn",
			Message:  "Source and destination effective time-zone settings differ; TIMESTAMP rendering and time-sensitive functions may not behave the same after cutover.",
			Proposal: "Review system_time_zone, global time_zone, and app session time_zone handling before claiming temporal compatibility.",
		})
	}
	if report.MixedTemporalTableCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "mixed_timestamp_datetime_tables_detected",
			Severity: "warn",
			Message:  fmt.Sprintf("Detected %d table(s) that mix TIMESTAMP and DATETIME columns.", report.MixedTemporalTableCount),
			Proposal: "Rehearse application behavior on these tables explicitly; mixed TIMESTAMP/DATETIME usage often hides time-zone interpretation bugs.",
		})
	}
	for _, item := range report.Tables {
		severity := "info"
		code := "temporal_table_inventory"
		if item.TimestampCount > 0 && item.DatetimeCount > 0 {
			severity = "warn"
			code = "mixed_timestamp_datetime_table"
		} else if item.TimestampCount > 0 && normalizeTimeZoneSetting(report.SourceSystemTimeZone, report.SourceGlobalTimeZone) != normalizeTimeZoneSetting(report.DestSystemTimeZone, report.DestGlobalTimeZone) {
			severity = "warn"
			code = "timestamp_table_timezone_review"
		}
		findings = append(findings, compat.Finding{
			Code:     code,
			Severity: severity,
			Message:  fmt.Sprintf("Temporal inventory for %s.%s: TIMESTAMP=%d DATETIME=%d.", item.Database, item.Table, item.TimestampCount, item.DatetimeCount),
			Proposal: item.Proposal,
		})
	}
	if len(report.Tables) == 0 {
		findings = append(findings, compat.Finding{
			Code:     "timezone_temporal_inventory_clean",
			Severity: "info",
			Message:  "No TIMESTAMP or DATETIME columns were found in selected scope.",
			Proposal: "Keep the time-zone inventory anyway; server settings still matter for time-sensitive routines and functions outside v1 object scope.",
		})
	}
	return findings
}

func normalizeTimeZoneSetting(systemTimeZone string, globalTimeZone string) string {
	global := strings.TrimSpace(globalTimeZone)
	if global == "" {
		return strings.TrimSpace(systemTimeZone)
	}
	if strings.EqualFold(global, "SYSTEM") {
		return strings.TrimSpace(systemTimeZone)
	}
	return global
}

func sortTemporalTableIssues(items []temporalTableIssue) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Database != items[j].Database {
			return items[i].Database < items[j].Database
		}
		return items[i].Table < items[j].Table
	})
}
