package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var systemSchemas = map[string]struct{}{
	"information_schema": {},
	"performance_schema": {},
	"mysql":              {},
	"sys":                {},
}

// CopyOptions controls schema extraction and apply behavior.
type CopyOptions struct {
	IncludeDatabases  []string
	ExcludeDatabases  []string
	IncludeTables     bool
	IncludeViews      bool
	DestEmptyRequired bool
}

// CopySummary reports how many objects were applied.
type CopySummary struct {
	Databases  int
	Tables     int
	Views      int
	Statements int
}

// CopySchema extracts schema DDL from source and applies it to destination.
func CopySchema(ctx context.Context, source *sql.DB, dest *sql.DB, opts CopyOptions) (CopySummary, error) {
	if source == nil || dest == nil {
		return CopySummary{}, errors.New("source and destination connections are required")
	}
	if !opts.IncludeTables && !opts.IncludeViews {
		return CopySummary{}, errors.New("at least one schema object type must be enabled (tables or views)")
	}

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return CopySummary{}, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := SelectDatabases(allDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	if opts.DestEmptyRequired {
		count, err := countUserTables(ctx, dest, opts.ExcludeDatabases)
		if err != nil {
			return CopySummary{}, fmt.Errorf("check destination emptiness: %w", err)
		}
		if count > 0 {
			return CopySummary{}, fmt.Errorf("destination is not empty: found %d user tables", count)
		}
	}

	summary := CopySummary{}
	for _, databaseName := range selectedDatabases {
		statements, tableCount, viewCount, err := extractCreateStatements(ctx, source, databaseName, opts.IncludeTables, opts.IncludeViews)
		if err != nil {
			return summary, fmt.Errorf("extract schema for %s: %w", databaseName, err)
		}
		if len(statements) == 0 {
			continue
		}
		if err := applyStatements(ctx, dest, databaseName, statements); err != nil {
			return summary, fmt.Errorf("apply schema for %s: %w", databaseName, err)
		}

		summary.Databases++
		summary.Tables += tableCount
		summary.Views += viewCount
		summary.Statements += len(statements)
	}

	return summary, nil
}

func listDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	var out []string
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

func countUserTables(ctx context.Context, db *sql.DB, excluded []string) (int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_SCHEMA, COUNT(*)
		FROM information_schema.TABLES
		GROUP BY TABLE_SCHEMA
	`)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = rows.Close()
	}()

	excludedSet := schemaSet(excluded)
	count := 0
	for rows.Next() {
		var schemaName string
		var tableCount int
		if err := rows.Scan(&schemaName, &tableCount); err != nil {
			return 0, err
		}
		if isExcludedSchema(schemaName, excludedSet) {
			continue
		}
		count += tableCount
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func extractCreateStatements(ctx context.Context, source *sql.DB, databaseName string, includeTables bool, includeViews bool) ([]string, int, int, error) {
	var statements []string
	tableCount := 0
	viewCount := 0

	if includeTables {
		tableNames, err := queryObjectNames(ctx, source, databaseName, "BASE TABLE")
		if err != nil {
			return nil, 0, 0, err
		}
		for _, tableName := range tableNames {
			stmt, err := fetchCreateStatement(ctx, source, fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName)))
			if err != nil {
				return nil, 0, 0, err
			}
			statements = append(statements, stmt)
			tableCount++
		}
	}

	if includeViews {
		viewNames, err := queryObjectNames(ctx, source, databaseName, "VIEW")
		if err != nil {
			return nil, 0, 0, err
		}
		for _, viewName := range viewNames {
			stmt, err := fetchCreateStatement(ctx, source, fmt.Sprintf("SHOW CREATE VIEW %s.%s", quoteIdentifier(databaseName), quoteIdentifier(viewName)))
			if err != nil {
				return nil, 0, 0, err
			}
			statements = append(statements, stmt)
			viewCount++
		}
	}

	return statements, tableCount, viewCount, nil
}

func queryObjectNames(ctx context.Context, source *sql.DB, databaseName string, objectType string) ([]string, error) {
	rows, err := source.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = ?
		ORDER BY TABLE_NAME
	`, databaseName, objectType)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := []string{}
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

func fetchCreateStatement(ctx context.Context, source *sql.DB, query string) (string, error) {
	rows, err := source.QueryContext(ctx, query)
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
		return "", errors.New("unexpected SHOW CREATE result format")
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

	statement := string(values[1])
	if strings.TrimSpace(statement) == "" {
		return "", errors.New("empty CREATE statement")
	}
	return statement, nil
}

func applyStatements(ctx context.Context, dest *sql.DB, databaseName string, statements []string) error {
	conn, err := dest.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = conn.Close()
	}()

	if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteIdentifier(databaseName))); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("USE %s", quoteIdentifier(databaseName))); err != nil {
		return err
	}

	for _, stmt := range statements {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// SelectDatabases deterministically applies include/exclude filters to available databases.
func SelectDatabases(all []string, include []string, exclude []string) []string {
	includeSet := schemaSet(include)
	excludeSet := schemaSet(exclude)

	result := make([]string, 0, len(all))
	for _, name := range all {
		if isExcludedSchema(name, excludeSet) {
			continue
		}
		if len(includeSet) > 0 {
			if _, ok := includeSet[strings.ToLower(strings.TrimSpace(name))]; !ok {
				continue
			}
		}
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func schemaSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func isExcludedSchema(name string, excluded map[string]struct{}) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := systemSchemas[normalized]; ok {
		return true
	}
	_, ok := excluded[normalized]
	return ok
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
