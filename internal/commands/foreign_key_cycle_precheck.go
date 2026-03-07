package commands

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/schema"
)

type foreignKeyCyclePrecheckReport struct {
	Name       string                 `json:"name"`
	IssueCount int                    `json:"issue_count"`
	Issues     []foreignKeyCycleIssue `json:"issues,omitempty"`
	Findings   []compat.Finding       `json:"findings,omitempty"`
}

type foreignKeyCycleIssue struct {
	Database string   `json:"database"`
	Tables   []string `json:"tables"`
}

func runForeignKeyCyclePrecheck(
	ctx context.Context,
	source *sql.DB,
	includeDatabases []string,
	excludeDatabases []string,
) (foreignKeyCyclePrecheckReport, error) {
	report := foreignKeyCyclePrecheckReport{
		Name: "foreign-key-cycles",
	}
	if source == nil {
		return report, fmt.Errorf("source connection is required")
	}

	databases, err := listSelectableDatabases(ctx, source, includeDatabases, excludeDatabases)
	if err != nil {
		return report, err
	}

	for _, databaseName := range databases {
		dependencies, err := loadDatabaseForeignKeyDependencies(ctx, source, databaseName)
		if err != nil {
			return report, fmt.Errorf("inspect foreign keys for %s: %w", databaseName, err)
		}
		cycles := detectForeignKeyCycles(dependencies)
		for _, cycle := range cycles {
			report.Issues = append(report.Issues, foreignKeyCycleIssue{
				Database: databaseName,
				Tables:   cycle,
			})
		}
	}

	sort.Slice(report.Issues, func(i, j int) bool {
		if report.Issues[i].Database != report.Issues[j].Database {
			return report.Issues[i].Database < report.Issues[j].Database
		}
		return strings.Join(report.Issues[i].Tables, ",") < strings.Join(report.Issues[j].Tables, ",")
	})
	report.IssueCount = len(report.Issues)
	report.Findings = buildForeignKeyCycleFindings(report)
	return report, nil
}

func buildForeignKeyCycleFindings(report foreignKeyCyclePrecheckReport) []compat.Finding {
	if report.IssueCount == 0 {
		return []compat.Finding{{
			Code:     "foreign_key_cycle_inventory_clean",
			Severity: "info",
			Message:  "No intra-database foreign-key cycles detected in selected scope.",
			Proposal: "Proceed with normal schema/data validation gates.",
		}}
	}

	findings := make([]compat.Finding, 0, report.IssueCount+1)
	findings = append(findings, compat.Finding{
		Code:     "foreign_key_cycles_detected",
		Severity: "error",
		Message:  fmt.Sprintf("Detected %d intra-database foreign-key cycle group(s) in selected scope.", report.IssueCount),
		Proposal: "For each cycle, create/load the affected tables without the cyclic constraints, then add those constraints in a controlled manual post-step before cutover.",
	})
	for _, issue := range report.Issues {
		findings = append(findings, compat.Finding{
			Code:     "foreign_key_cycle_group",
			Severity: "error",
			Message:  fmt.Sprintf("Database %s contains a cyclic foreign-key group across tables %s.", issue.Database, strings.Join(issue.Tables, ", ")),
			Proposal: fmt.Sprintf("Generate non-cyclic CREATE TABLE statements for %s, load data, then apply the cyclic FOREIGN KEY constraints with post-load ALTER TABLE statements.", strings.Join(issue.Tables, ", ")),
		})
	}
	return findings
}

func listSelectableDatabases(ctx context.Context, db *sql.DB, includeDatabases []string, excludeDatabases []string) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	all := make([]string, 0, 16)
	for rows.Next() {
		var schemaName string
		if err := rows.Scan(&schemaName); err != nil {
			return nil, err
		}
		all = append(all, schemaName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return schema.SelectDatabases(all, includeDatabases, excludeDatabases), nil
}

func loadDatabaseForeignKeyDependencies(ctx context.Context, source *sql.DB, databaseName string) (map[string]map[string]struct{}, error) {
	tableRows, err := source.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tableRows.Close()
	}()

	dependencies := map[string]map[string]struct{}{}
	tableSet := map[string]struct{}{}
	for tableRows.Next() {
		var tableName string
		if err := tableRows.Scan(&tableName); err != nil {
			return nil, err
		}
		dependencies[tableName] = map[string]struct{}{}
		tableSet[tableName] = struct{}{}
	}
	if err := tableRows.Err(); err != nil {
		return nil, err
	}

	if len(tableSet) < 2 {
		return dependencies, nil
	}

	rows, err := source.QueryContext(ctx, `
		SELECT TABLE_NAME, REFERENCED_TABLE_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = ?
		  AND REFERENCED_TABLE_NAME IS NOT NULL
		  AND (REFERENCED_TABLE_SCHEMA IS NULL OR REFERENCED_TABLE_SCHEMA = TABLE_SCHEMA)
		ORDER BY TABLE_NAME, REFERENCED_TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
		var tableName string
		var referencedTableName string
		if err := rows.Scan(&tableName, &referencedTableName); err != nil {
			return nil, err
		}
		if tableName == referencedTableName {
			continue
		}
		if _, ok := tableSet[tableName]; !ok {
			continue
		}
		if _, ok := tableSet[referencedTableName]; !ok {
			continue
		}
		dependencies[tableName][referencedTableName] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return dependencies, nil
}

func detectForeignKeyCycles(dependencies map[string]map[string]struct{}) [][]string {
	index := 0
	stack := make([]string, 0, len(dependencies))
	onStack := map[string]bool{}
	indexes := map[string]int{}
	lowlink := map[string]int{}
	components := make([][]string, 0)

	var strongConnect func(node string)
	strongConnect = func(node string) {
		indexes[node] = index
		lowlink[node] = index
		index++
		stack = append(stack, node)
		onStack[node] = true

		neighbors := make([]string, 0, len(dependencies[node]))
		for dep := range dependencies[node] {
			neighbors = append(neighbors, dep)
		}
		sort.Strings(neighbors)

		for _, neighbor := range neighbors {
			if _, seen := indexes[neighbor]; !seen {
				strongConnect(neighbor)
				if lowlink[neighbor] < lowlink[node] {
					lowlink[node] = lowlink[neighbor]
				}
			} else if onStack[neighbor] && indexes[neighbor] < lowlink[node] {
				lowlink[node] = indexes[neighbor]
			}
		}

		if lowlink[node] != indexes[node] {
			return
		}

		component := make([]string, 0, 4)
		for {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == node {
				break
			}
		}
		sort.Strings(component)
		if len(component) > 1 {
			components = append(components, component)
		}
	}

	nodes := make([]string, 0, len(dependencies))
	for node := range dependencies {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		if _, seen := indexes[node]; !seen {
			strongConnect(node)
		}
	}

	sort.Slice(components, func(i, j int) bool {
		return strings.Join(components[i], ",") < strings.Join(components[j], ",")
	})
	return components
}
