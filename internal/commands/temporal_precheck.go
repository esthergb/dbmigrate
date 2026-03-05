package commands

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/version"
)

type zeroDateDefaultIssue struct {
	Database       string `json:"database"`
	Table          string `json:"table"`
	Column         string `json:"column"`
	DataType       string `json:"data_type"`
	ColumnType     string `json:"column_type"`
	DefaultValue   string `json:"default_value"`
	ProposedFixSQL string `json:"proposed_fix_sql"`
}

type zeroDateDefaultsPrecheckReport struct {
	Name                string                 `json:"name"`
	Incompatible        bool                   `json:"incompatible"`
	DestinationSQLMode  string                 `json:"destination_sql_mode"`
	DestinationEnforced bool                   `json:"destination_enforced"`
	IssueCount          int                    `json:"issue_count"`
	Issues              []zeroDateDefaultIssue `json:"issues,omitempty"`
	Findings            []compat.Finding       `json:"findings,omitempty"`
}

type migratePrecheckResult struct {
	Command   string                         `json:"command"`
	Status    string                         `json:"status"`
	Precheck  zeroDateDefaultsPrecheckReport `json:"precheck"`
	Timestamp time.Time                      `json:"timestamp"`
	Version   string                         `json:"version"`
}

func runZeroDateDefaultsPrecheck(
	ctx context.Context,
	source *sql.DB,
	dest *sql.DB,
	includeDatabases []string,
	excludeDatabases []string,
) (zeroDateDefaultsPrecheckReport, error) {
	report := zeroDateDefaultsPrecheckReport{
		Name: "zero-date-defaults",
	}

	sqlMode, err := querySQLMode(ctx, dest)
	if err != nil {
		return report, fmt.Errorf("read destination sql_mode: %w", err)
	}
	report.DestinationSQLMode = sqlMode
	report.DestinationEnforced = destinationEnforcesZeroDateStrict(sqlMode)
	if !report.DestinationEnforced {
		return report, nil
	}

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return report, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := schema.SelectDatabases(allDatabases, includeDatabases, excludeDatabases)
	if len(selectedDatabases) == 0 {
		return report, nil
	}

	issues, err := queryZeroDateDefaultIssues(ctx, source, selectedDatabases)
	if err != nil {
		return report, err
	}
	report.IssueCount = len(issues)
	if len(issues) == 0 {
		return report, nil
	}
	report.Issues = issues
	report.Incompatible = true
	report.Findings = buildZeroDatePrecheckFindings(report)
	return report, nil
}

func querySQLMode(ctx context.Context, db *sql.DB) (string, error) {
	var sqlMode sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&sqlMode); err != nil {
		return "", err
	}
	return strings.TrimSpace(sqlMode.String), nil
}

func destinationEnforcesZeroDateStrict(sqlMode string) bool {
	hasStrict := sqlModeContains(sqlMode, "STRICT_TRANS_TABLES") || sqlModeContains(sqlMode, "STRICT_ALL_TABLES")
	hasNoZero := sqlModeContains(sqlMode, "NO_ZERO_DATE") || sqlModeContains(sqlMode, "NO_ZERO_IN_DATE")
	return hasStrict && hasNoZero
}

func sqlModeContains(sqlMode string, token string) bool {
	for _, mode := range strings.Split(strings.ToUpper(sqlMode), ",") {
		if strings.TrimSpace(mode) == token {
			return true
		}
	}
	return false
}

func listDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := make([]string, 0, 8)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryZeroDateDefaultIssues(ctx context.Context, source *sql.DB, databases []string) ([]zeroDateDefaultIssue, error) {
	placeholders := make([]string, 0, len(databases))
	args := make([]any, 0, len(databases))
	for _, databaseName := range databases {
		placeholders = append(placeholders, "?")
		args = append(args, databaseName)
	}

	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, COLUMN_DEFAULT
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		  AND COLUMN_DEFAULT IS NOT NULL
		  AND DATA_TYPE IN ('date', 'datetime', 'timestamp')
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, strings.Join(placeholders, ","))

	rows, err := source.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query source temporal defaults: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	issues := make([]zeroDateDefaultIssue, 0, 16)
	for rows.Next() {
		var (
			databaseName string
			tableName    string
			columnName   string
			dataType     string
			columnType   string
			defaultValue sql.NullString
		)
		if err := rows.Scan(&databaseName, &tableName, &columnName, &dataType, &columnType, &defaultValue); err != nil {
			return nil, err
		}

		normalizedDefault := strings.TrimSpace(strings.Trim(defaultValue.String, `"'`))
		if !temporalDefaultContainsZeroDate(normalizedDefault) {
			continue
		}

		issues = append(issues, zeroDateDefaultIssue{
			Database:     databaseName,
			Table:        tableName,
			Column:       columnName,
			DataType:     strings.ToLower(strings.TrimSpace(dataType)),
			ColumnType:   strings.TrimSpace(columnType),
			DefaultValue: normalizedDefault,
			ProposedFixSQL: autoFixAlterDefaultSQL(
				databaseName,
				tableName,
				columnName,
				strings.ToLower(strings.TrimSpace(dataType)),
			),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}

func temporalDefaultContainsZeroDate(defaultValue string) bool {
	v := strings.TrimSpace(strings.Trim(defaultValue, `"'`))
	if v == "" {
		return false
	}

	datePart := v
	if idx := strings.IndexAny(v, " T"); idx >= 0 {
		datePart = v[:idx]
	}
	if len(datePart) != 10 {
		return false
	}
	if datePart[4] != '-' || datePart[7] != '-' {
		return false
	}
	year := datePart[0:4]
	month := datePart[5:7]
	day := datePart[8:10]
	return year == "0000" || month == "00" || day == "00"
}

func autoFixAlterDefaultSQL(databaseName string, tableName string, columnName string, dataType string) string {
	replacement := "1970-01-01 00:00:01"
	if dataType == "date" {
		replacement = "1970-01-01"
	}
	return fmt.Sprintf(
		"ALTER TABLE %s.%s ALTER COLUMN %s SET DEFAULT '%s';",
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
		quoteIdentifier(columnName),
		replacement,
	)
}

func buildZeroDatePrecheckFindings(report zeroDateDefaultsPrecheckReport) []compat.Finding {
	if report.IssueCount == 0 {
		return nil
	}

	findings := make([]compat.Finding, 0, report.IssueCount+1)
	findings = append(findings, compat.Finding{
		Code:     "zero_date_defaults_incompatible",
		Severity: "error",
		Message: fmt.Sprintf(
			"Detected %d zero-date temporal default(s) in source schema while destination sql_mode enforces strict NO_ZERO_DATE/NO_ZERO_IN_DATE checks.",
			report.IssueCount,
		),
		Proposal: "Apply the auto-fix ALTER TABLE ... SET DEFAULT statements listed in findings, then rerun plan/migrate.",
	})

	for _, issue := range report.Issues {
		findings = append(findings, compat.Finding{
			Code:     "zero_date_default_column",
			Severity: "error",
			Message: fmt.Sprintf(
				"Source %s.%s column %s (%s) uses incompatible default %q under destination sql_mode %q.",
				issue.Database,
				issue.Table,
				issue.Column,
				issue.ColumnType,
				issue.DefaultValue,
				report.DestinationSQLMode,
			),
			Proposal: fmt.Sprintf("Auto-fix candidate: %s", issue.ProposedFixSQL),
		})
	}
	return findings
}

func writeMigratePrecheckReport(out io.Writer, cfg config.RuntimeConfig, report zeroDateDefaultsPrecheckReport) error {
	payload := migratePrecheckResult{
		Command:   "migrate",
		Status:    "incompatible",
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
		"[migrate] status=incompatible precheck=%s destination_enforced=%v destination_sql_mode=%q findings=%d\n",
		report.Name,
		report.DestinationEnforced,
		report.DestinationSQLMode,
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
