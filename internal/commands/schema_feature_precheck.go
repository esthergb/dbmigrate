package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/version"
)

var systemVersioningClauseRe = regexp.MustCompile(`(?i)\bWITH\s+SYSTEM\s+VERSIONING\b`)

type schemaFeaturePrecheckReport struct {
	Name                      string                 `json:"name"`
	Incompatible              bool                   `json:"incompatible"`
	SourceVersion             string                 `json:"source_version"`
	DestVersion               string                 `json:"dest_version"`
	JSONColumnCount           int                    `json:"json_column_count"`
	SequenceCount             int                    `json:"sequence_count"`
	SystemVersionedTableCount int                    `json:"system_versioned_table_count"`
	JSONColumns               []jsonColumnIssue      `json:"json_columns,omitempty"`
	Sequences                 []sequenceIssue        `json:"sequences,omitempty"`
	SystemVersionedTables     []systemVersionedIssue `json:"system_versioned_tables,omitempty"`
	Findings                  []compat.Finding       `json:"findings,omitempty"`
}

type schemaFeaturePrecheckResult struct {
	Command   string                      `json:"command"`
	Status    string                      `json:"status"`
	Precheck  schemaFeaturePrecheckReport `json:"precheck"`
	Timestamp time.Time                   `json:"timestamp"`
	Version   string                      `json:"version"`
}

type jsonColumnIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Column   string `json:"column"`
	Proposal string `json:"proposal"`
}

type sequenceIssue struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Proposal string `json:"proposal"`
}

type systemVersionedIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Proposal string `json:"proposal"`
}

func runSchemaFeaturePrecheck(
	ctx context.Context,
	source *sql.DB,
	sourceInstance compat.Instance,
	destInstance compat.Instance,
	includeDatabases []string,
	excludeDatabases []string,
) (schemaFeaturePrecheckReport, error) {
	report := schemaFeaturePrecheckReport{
		Name:          "schema-features",
		SourceVersion: sourceInstance.RawVersion,
		DestVersion:   destInstance.RawVersion,
	}
	if source == nil {
		return report, fmt.Errorf("source connection is required")
	}

	databases, err := listSelectableDatabases(ctx, source, includeDatabases, excludeDatabases)
	if err != nil {
		return report, err
	}
	if len(databases) == 0 {
		report.Findings = []compat.Finding{{
			Code:     "schema_feature_no_selected_databases",
			Severity: "info",
			Message:  "No user databases selected for schema feature inventory.",
			Proposal: "Use --databases to narrow scope or rerun without filters to inventory all user schemas.",
		}}
		return report, nil
	}

	if sourceInstance.Engine != destInstance.Engine {
		report.JSONColumns, err = detectJSONColumns(ctx, source, databases)
		if err != nil {
			return report, err
		}
		report.JSONColumnCount = len(report.JSONColumns)
	}

	if sourceInstance.Engine == compat.EngineMariaDB {
		report.Sequences, err = detectMariaDBSequences(ctx, source, databases)
		if err != nil {
			return report, err
		}
		report.SequenceCount = len(report.Sequences)

		report.SystemVersionedTables, err = detectSystemVersionedTables(ctx, source, databases)
		if err != nil {
			return report, err
		}
		report.SystemVersionedTableCount = len(report.SystemVersionedTables)
	}

	report.Findings = buildSchemaFeaturePrecheckFindings(sourceInstance, destInstance, report)
	for _, finding := range report.Findings {
		if finding.Severity == "error" {
			report.Incompatible = true
			break
		}
	}
	return report, nil
}

func detectJSONColumns(ctx context.Context, source *sql.DB, databases []string) ([]jsonColumnIssue, error) {
	issues := make([]jsonColumnIssue, 0, 8)
	for _, databaseName := range databases {
		rows, err := source.QueryContext(ctx, `
			SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = ? AND DATA_TYPE = 'json'
			ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
		`, databaseName)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var issue jsonColumnIssue
			if err := rows.Scan(&issue.Database, &issue.Table, &issue.Column); err != nil {
				_ = rows.Close()
				return nil, err
			}
			issue.Proposal = fmt.Sprintf("Review %s.%s.%s and convert JSON columns to an agreed text/json representation or validate semantic compatibility before cross-engine cutover.", issue.Database, issue.Table, issue.Column)
			issues = append(issues, issue)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return issues, nil
}

func detectMariaDBSequences(ctx context.Context, source *sql.DB, databases []string) ([]sequenceIssue, error) {
	issues := make([]sequenceIssue, 0, 4)
	for _, databaseName := range databases {
		rows, err := source.QueryContext(ctx, `
			SELECT SEQUENCE_SCHEMA, SEQUENCE_NAME
			FROM information_schema.SEQUENCES
			WHERE SEQUENCE_SCHEMA = ?
			ORDER BY SEQUENCE_NAME
		`, databaseName)
		if err != nil {
			if isTableNotFoundError(err) {
				continue
			}
			return nil, err
		}
		for rows.Next() {
			var issue sequenceIssue
			if err := rows.Scan(&issue.Database, &issue.Name); err != nil {
				_ = rows.Close()
				return nil, err
			}
			issue.Proposal = fmt.Sprintf("Replace MariaDB sequence %s.%s with destination-compatible key generation before migration.", issue.Database, issue.Name)
			issues = append(issues, issue)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return issues, nil
}

func detectSystemVersionedTables(ctx context.Context, source *sql.DB, databases []string) ([]systemVersionedIssue, error) {
	issues := make([]systemVersionedIssue, 0, 4)
	for _, databaseName := range databases {
		rows, err := source.QueryContext(ctx, `
			SELECT TABLE_NAME
			FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
			ORDER BY TABLE_NAME
		`, databaseName)
		if err != nil {
			return nil, err
		}
		tableNames := make([]string, 0, 16)
		for rows.Next() {
			var tableName string
			if err := rows.Scan(&tableName); err != nil {
				_ = rows.Close()
				return nil, err
			}
			tableNames = append(tableNames, tableName)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()

		for _, tableName := range tableNames {
			stmt, err := fetchShowCreateTable(ctx, source, databaseName, tableName)
			if err != nil {
				return nil, err
			}
			if !systemVersioningClauseRe.MatchString(stmt) {
				continue
			}
			issues = append(issues, systemVersionedIssue{
				Database: databaseName,
				Table:    tableName,
				Proposal: fmt.Sprintf("Extract a plain-table representation or disable system versioning for %s.%s before moving it outside a MariaDB-only lane.", databaseName, tableName),
			})
		}
	}
	return issues, nil
}

func fetchShowCreateTable(ctx context.Context, db *sql.DB, databaseName string, tableName string) (string, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", databaseName, tableName))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns, err := rows.Columns()
	if err != nil {
		return "", err
	}
	if len(columns) < 2 {
		return "", errors.New("unexpected SHOW CREATE TABLE result format")
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", sql.ErrNoRows
	}

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}
	if err := rows.Scan(scanArgs...); err != nil {
		return "", err
	}
	stmt := strings.TrimSpace(string(values[1]))
	if stmt == "" {
		return "", errors.New("empty CREATE statement")
	}
	return stmt, nil
}

func buildSchemaFeaturePrecheckFindings(source compat.Instance, dest compat.Instance, report schemaFeaturePrecheckReport) []compat.Finding {
	findings := make([]compat.Finding, 0, report.JSONColumnCount+report.SequenceCount+report.SystemVersionedTableCount+4)

	if report.JSONColumnCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "cross_engine_json_columns_detected",
			Severity: "error",
			Message:  fmt.Sprintf("Detected %d JSON column(s) in a cross-engine path (%s -> %s).", report.JSONColumnCount, source.Engine, dest.Engine),
			Proposal: "Treat cross-engine JSON as incompatible in v1. Convert the affected columns to an agreed destination-safe representation or use an engine-compatible route.",
		})
		for _, issue := range report.JSONColumns {
			findings = append(findings, compat.Finding{
				Code:     "cross_engine_json_column",
				Severity: "error",
				Message:  fmt.Sprintf("Cross-engine JSON column detected at %s.%s.%s.", issue.Database, issue.Table, issue.Column),
				Proposal: issue.Proposal,
			})
		}
	}

	if report.SequenceCount > 0 {
		severity := "warn"
		code := "mariadb_sequences_detected"
		message := fmt.Sprintf("Detected %d MariaDB sequence object(s) in selected scope.", report.SequenceCount)
		proposal := "Keep sequences inside MariaDB-only lanes or replace them with destination-compatible key generation before cutover."
		if dest.Engine != compat.EngineMariaDB {
			severity = "error"
			code = "mariadb_sequences_unsupported_on_destination"
			message = fmt.Sprintf("Detected %d MariaDB sequence object(s) but destination engine is %s.", report.SequenceCount, dest.Engine)
			proposal = "MariaDB sequences are incompatible on this destination in v1; replace or materialize them before migration."
		}
		findings = append(findings, compat.Finding{
			Code:     code,
			Severity: severity,
			Message:  message,
			Proposal: proposal,
		})
		for _, issue := range report.Sequences {
			findings = append(findings, compat.Finding{
				Code:     "mariadb_sequence_object",
				Severity: severity,
				Message:  fmt.Sprintf("MariaDB sequence detected at %s.%s.", issue.Database, issue.Name),
				Proposal: issue.Proposal,
			})
		}
	}

	if report.SystemVersionedTableCount > 0 {
		severity := "warn"
		code := "system_versioned_tables_detected"
		message := fmt.Sprintf("Detected %d system-versioned table(s) in selected scope.", report.SystemVersionedTableCount)
		proposal := "Rehearse versioned-table behavior carefully; binlog/replication semantics can diverge even inside MariaDB lanes."
		if dest.Engine != compat.EngineMariaDB {
			severity = "error"
			code = "system_versioned_tables_unsupported_on_destination"
			message = fmt.Sprintf("Detected %d system-versioned table(s) but destination engine is %s.", report.SystemVersionedTableCount, dest.Engine)
			proposal = "System-versioned MariaDB tables are incompatible on this destination in v1; convert them to plain tables or extract history separately before migration."
		}
		findings = append(findings, compat.Finding{
			Code:     code,
			Severity: severity,
			Message:  message,
			Proposal: proposal,
		})
		for _, issue := range report.SystemVersionedTables {
			findings = append(findings, compat.Finding{
				Code:     "system_versioned_table",
				Severity: severity,
				Message:  fmt.Sprintf("System-versioned table detected at %s.%s.", issue.Database, issue.Table),
				Proposal: issue.Proposal,
			})
		}
	}

	if len(findings) == 0 {
		findings = append(findings, compat.Finding{
			Code:     "schema_feature_inventory_clean",
			Severity: "info",
			Message:  "No JSON cross-engine risk, MariaDB sequence objects, or system-versioned tables detected in selected scope.",
			Proposal: "Proceed with the remaining v1 validation gates.",
		})
	}

	return findings
}

func writeSchemaFeaturePrecheckReport(out io.Writer, cfg config.RuntimeConfig, report schemaFeaturePrecheckReport) error {
	status := "warning"
	if report.Incompatible {
		status = "incompatible"
	}

	payload := schemaFeaturePrecheckResult{
		Command:   "migrate",
		Status:    status,
		Precheck:  report,
		Timestamp: time.Now().UTC(),
		Version:   version.Version,
	}
	if cfg.JSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	if _, err := fmt.Fprintf(
		out,
		"[migrate] status=%s precheck=%s json_columns=%d sequences=%d system_versioned_tables=%d findings=%d\n",
		status,
		report.Name,
		report.JSONColumnCount,
		report.SequenceCount,
		report.SystemVersionedTableCount,
		len(report.Findings),
	); err != nil {
		return err
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(
			out,
			"[migrate] precheck %s code=%s message=%s proposal=%s\n",
			finding.Severity,
			finding.Code,
			finding.Message,
			finding.Proposal,
		); err != nil {
			return err
		}
	}
	return nil
}

func isTableNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "1109") ||
		strings.Contains(msg, "er_unknown_table")
}
