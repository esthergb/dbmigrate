package commands

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/esthergb/dbmigrate/internal/compat"
)

type dataShapePrecheckReport struct {
	Name                     string                    `json:"name"`
	Incompatible             bool                      `json:"incompatible"`
	KeylessTableCount        int                       `json:"keyless_table_count"`
	RepresentationRiskCount  int                       `json:"representation_risk_count"`
	KeylessTables            []keylessTableIssue       `json:"keyless_tables,omitempty"`
	RepresentationRiskTables []representationRiskIssue `json:"representation_risk_tables,omitempty"`
	Findings                 []compat.Finding          `json:"findings,omitempty"`
}

type keylessTableIssue struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Proposal string `json:"proposal"`
}

type representationRiskIssue struct {
	Database                  string   `json:"database"`
	Table                     string   `json:"table"`
	ApproximateNumericColumns int      `json:"approximate_numeric_columns,omitempty"`
	TemporalColumns           int      `json:"temporal_columns,omitempty"`
	JSONColumns               int      `json:"json_columns,omitempty"`
	CollationSensitiveColumns int      `json:"collation_sensitive_columns,omitempty"`
	Notes                     []string `json:"notes,omitempty"`
	Proposal                  string   `json:"proposal"`
}

func runDataShapePrecheck(
	ctx context.Context,
	source *sql.DB,
	includeDatabases []string,
	excludeDatabases []string,
) (dataShapePrecheckReport, error) {
	report := dataShapePrecheckReport{Name: "data-shape"}
	if source == nil {
		return report, fmt.Errorf("source connection is required")
	}
	databases, err := listSelectableDatabases(ctx, source, includeDatabases, excludeDatabases)
	if err != nil {
		return report, err
	}
	if len(databases) == 0 {
		report.Findings = []compat.Finding{{
			Code:     "data_shape_no_selected_databases",
			Severity: "info",
			Message:  "No user databases selected for data-shape validation.",
			Proposal: "Use --databases to narrow scope or rerun without filters to inspect all user schemas.",
		}}
		return report, nil
	}

	for _, databaseName := range databases {
		tableNames, err := listBaseTableNamesForPrecheck(ctx, source, databaseName)
		if err != nil {
			return report, err
		}
		for _, tableName := range tableNames {
			keyColumns, err := listStableKeyColumnsForPrecheck(ctx, source, databaseName, tableName)
			if err != nil {
				return report, fmt.Errorf("stable key check for %s.%s: %w", databaseName, tableName, err)
			}
			if len(keyColumns) == 0 {
				report.KeylessTables = append(report.KeylessTables, keylessTableIssue{
					Database: databaseName,
					Table:    tableName,
					Proposal: fmt.Sprintf("Add a primary key or non-null unique key to %s.%s before v1 baseline migration or deterministic verify modes.", databaseName, tableName),
				})
			}
		}
	}

	issues, err := queryRepresentationRiskIssues(ctx, source, databases)
	if err != nil {
		return report, err
	}
	report.RepresentationRiskTables = issues
	report.KeylessTableCount = len(report.KeylessTables)
	report.RepresentationRiskCount = len(report.RepresentationRiskTables)
	report.Findings = buildDataShapeFindings(report)
	report.Incompatible = report.KeylessTableCount > 0
	return report, nil
}

func listBaseTableNamesForPrecheck(ctx context.Context, source *sql.DB, databaseName string) ([]string, error) {
	rows, err := source.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, fmt.Errorf("list base tables for %s: %w", databaseName, err)
	}
	defer func() { _ = rows.Close() }()
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

func listStableKeyColumnsForPrecheck(ctx context.Context, queryer *sql.DB, databaseName string, tableName string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY
			CASE WHEN INDEX_NAME = 'PRIMARY' THEN 0 ELSE 1 END,
			NON_UNIQUE,
			INDEX_NAME,
			SEQ_IN_INDEX
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	indexes := map[string][]string{}
	nonUnique := map[string]int{}
	orderedIndexes := make([]string, 0, 4)
	for rows.Next() {
		var indexName string
		var uniqueFlag int
		var columnName string
		var seq int
		if err := rows.Scan(&indexName, &uniqueFlag, &columnName, &seq); err != nil {
			return nil, err
		}
		if _, ok := indexes[indexName]; !ok {
			orderedIndexes = append(orderedIndexes, indexName)
		}
		indexes[indexName] = append(indexes[indexName], columnName)
		nonUnique[indexName] = uniqueFlag
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	notNullColumns, err := listNotNullColumnsForPrecheck(ctx, queryer, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	for _, indexName := range orderedIndexes {
		if nonUnique[indexName] != 0 {
			continue
		}
		columns := indexes[indexName]
		if len(columns) == 0 {
			continue
		}
		eligible := true
		for _, column := range columns {
			if _, ok := notNullColumns[column]; !ok {
				eligible = false
				break
			}
		}
		if eligible {
			return columns, nil
		}
	}
	return nil, nil
}

func listNotNullColumnsForPrecheck(ctx context.Context, queryer *sql.DB, databaseName string, tableName string) (map[string]struct{}, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND IS_NULLABLE = 'NO'
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func queryRepresentationRiskIssues(ctx context.Context, source *sql.DB, databases []string) ([]representationRiskIssue, error) {
	placeholders, args := sqlPlaceholders(databases)
	rows, err := source.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME,
		       SUM(CASE WHEN DATA_TYPE IN ('float','double') THEN 1 ELSE 0 END) AS approx_numeric_count,
		       SUM(CASE WHEN DATA_TYPE IN ('timestamp','datetime') THEN 1 ELSE 0 END) AS temporal_count,
		       SUM(CASE WHEN DATA_TYPE = 'json' THEN 1 ELSE 0 END) AS json_count,
		       SUM(CASE WHEN COLLATION_NAME IS NOT NULL THEN 1 ELSE 0 END) AS collation_sensitive_count
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		GROUP BY TABLE_SCHEMA, TABLE_NAME
		HAVING approx_numeric_count > 0 OR temporal_count > 0 OR json_count > 0 OR collation_sensitive_count > 0
		ORDER BY TABLE_SCHEMA, TABLE_NAME
	`, placeholders), args...)
	if err != nil {
		return nil, fmt.Errorf("query representation-risk tables: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]representationRiskIssue, 0, 8)
	for rows.Next() {
		var issue representationRiskIssue
		if err := rows.Scan(&issue.Database, &issue.Table, &issue.ApproximateNumericColumns, &issue.TemporalColumns, &issue.JSONColumns, &issue.CollationSensitiveColumns); err != nil {
			return nil, err
		}
		if issue.ApproximateNumericColumns > 0 {
			issue.Notes = append(issue.Notes, "approximate numeric columns can hash or round differently across engines/clients")
		}
		if issue.TemporalColumns > 0 {
			issue.Notes = append(issue.Notes, "temporal columns depend on session time_zone rendering and canonicalization")
		}
		if issue.JSONColumns > 0 {
			issue.Notes = append(issue.Notes, "JSON values may need semantic rather than byte-for-byte comparison")
		}
		if issue.CollationSensitiveColumns > 0 {
			issue.Notes = append(issue.Notes, "text ordering and coercion can vary with collation families and clients")
		}
		issue.Proposal = fmt.Sprintf("Prefer canonicalized verify modes and keep representation-risk evidence for %s.%s before claiming semantic equivalence.", issue.Database, issue.Table)
		out = append(out, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func buildDataShapeFindings(report dataShapePrecheckReport) []compat.Finding {
	findings := make([]compat.Finding, 0, 4+report.KeylessTableCount+report.RepresentationRiskCount)
	if report.KeylessTableCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "stable_key_required_tables_detected",
			Severity: "error",
			Message:  fmt.Sprintf("Detected %d table(s) without a primary key or non-null unique key in selected scope.", report.KeylessTableCount),
			Proposal: "Add stable keys before v1 baseline migration or deterministic verify modes. Keyless tables are not supported for live baseline in v1.",
		})
		for _, issue := range report.KeylessTables {
			findings = append(findings, compat.Finding{
				Code:     "stable_key_required_table",
				Severity: "error",
				Message:  fmt.Sprintf("Table %s.%s has no primary key or non-null unique key.", issue.Database, issue.Table),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.RepresentationRiskCount > 0 {
		findings = append(findings, compat.Finding{
			Code:     "representation_sensitive_tables_detected",
			Severity: "warn",
			Message:  fmt.Sprintf("Detected %d representation-sensitive table(s) that deserve canonicalized verify review.", report.RepresentationRiskCount),
			Proposal: "Use canonicalized verify modes and keep the resulting evidence before claiming semantic equivalence across environments.",
		})
		for _, issue := range report.RepresentationRiskTables {
			findings = append(findings, compat.Finding{
				Code:     "representation_sensitive_table",
				Severity: "warn",
				Message:  fmt.Sprintf("Table %s.%s has representation-sensitive columns (approx_numeric=%d temporal=%d json=%d collation_sensitive=%d).", issue.Database, issue.Table, issue.ApproximateNumericColumns, issue.TemporalColumns, issue.JSONColumns, issue.CollationSensitiveColumns),
				Proposal: issue.Proposal,
			})
		}
	}
	if report.KeylessTableCount == 0 && report.RepresentationRiskCount == 0 {
		findings = append(findings, compat.Finding{
			Code:     "data_shape_inventory_clean",
			Severity: "info",
			Message:  "Data-shape validation did not reveal keyless baseline blockers or representation-sensitive verify hotspots in selected scope.",
			Proposal: "Keep the inventory artifact with the plan output as part of the v1 migration evidence set.",
		})
	}
	return findings
}
