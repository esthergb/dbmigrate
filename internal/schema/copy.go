package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var systemSchemas = map[string]struct{}{
	"information_schema": {},
	"performance_schema": {},
	"mysql":              {},
	"sys":                {},
}

var definerClauseRe = regexp.MustCompile("(?i)\\bDEFINER\\s*=\\s*`[^`]+`@`[^`]+`")

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

// DryRunSandboxOptions controls sandbox schema validation behavior.
type DryRunSandboxOptions struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	IncludeTables    bool
	IncludeViews     bool
	MapDatabase      func(sourceDatabase string) string
}

// DryRunSandboxSummary reports validated and failed sandbox DDL statements.
type DryRunSandboxSummary struct {
	Validated        int
	Failed           int
	CreatedDatabases []string
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

// ValidateSchemaInSandbox executes source DDL against mapped sandbox databases to validate compatibility.
func ValidateSchemaInSandbox(ctx context.Context, source *sql.DB, dest *sql.DB, opts DryRunSandboxOptions) (DryRunSandboxSummary, error) {
	if source == nil || dest == nil {
		return DryRunSandboxSummary{}, errors.New("source and destination connections are required")
	}
	if opts.MapDatabase == nil {
		return DryRunSandboxSummary{}, errors.New("MapDatabase is required")
	}
	if !opts.IncludeTables && !opts.IncludeViews {
		return DryRunSandboxSummary{}, errors.New("at least one schema object type must be enabled (tables or views)")
	}

	allDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return DryRunSandboxSummary{}, fmt.Errorf("list source databases: %w", err)
	}
	selectedDatabases := SelectDatabases(allDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)
	summary := DryRunSandboxSummary{
		CreatedDatabases: make([]string, 0, len(selectedDatabases)),
	}

	seenCreated := map[string]struct{}{}
	for _, sourceDatabase := range selectedDatabases {
		sandboxDatabase := strings.TrimSpace(opts.MapDatabase(sourceDatabase))
		if sandboxDatabase == "" {
			return summary, fmt.Errorf("sandbox database mapping is empty for source %q", sourceDatabase)
		}

		statements, _, _, err := extractCreateStatements(ctx, source, sourceDatabase, opts.IncludeTables, opts.IncludeViews)
		if err != nil {
			return summary, fmt.Errorf("extract schema for %s: %w", sourceDatabase, err)
		}
		if len(statements) == 0 {
			continue
		}

		conn, err := dest.Conn(ctx)
		if err != nil {
			return summary, err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", quoteIdentifier(sandboxDatabase))); err != nil {
			_ = conn.Close()
			summary.Failed++
			return summary, err
		}
		summary.Validated++

		if _, err := conn.ExecContext(ctx, fmt.Sprintf("USE %s", quoteIdentifier(sandboxDatabase))); err != nil {
			_ = conn.Close()
			summary.Failed++
			return summary, err
		}
		summary.Validated++

		if _, ok := seenCreated[sandboxDatabase]; !ok {
			seenCreated[sandboxDatabase] = struct{}{}
			summary.CreatedDatabases = append(summary.CreatedDatabases, sandboxDatabase)
		}

		for _, statement := range statements {
			rewritten := rewriteSchemaStatementForSandbox(statement, sourceDatabase, sandboxDatabase)
			rewritten = sanitizeCreateStatementForApply(rewritten)
			if _, err := conn.ExecContext(ctx, rewritten); err != nil {
				_ = conn.Close()
				summary.Failed++
				return summary, err
			}
			summary.Validated++
		}
		if err := conn.Close(); err != nil {
			summary.Failed++
			return summary, err
		}
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
		tableNames, err = orderTableNamesByForeignKeys(ctx, source, databaseName, tableNames)
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

func orderTableNamesByForeignKeys(ctx context.Context, source *sql.DB, databaseName string, tableNames []string) ([]string, error) {
	if len(tableNames) < 2 {
		return tableNames, nil
	}

	dependencies := make(map[string]map[string]struct{}, len(tableNames))
	tableSet := make(map[string]struct{}, len(tableNames))
	for _, tableName := range tableNames {
		dependencies[tableName] = map[string]struct{}{}
		tableSet[tableName] = struct{}{}
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

	ordered, cyclic := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	if len(cyclic) > 0 {
		return nil, fmt.Errorf(
			"incompatible_for_v1_foreign_key_cycle: %s contains cyclic foreign keys across tables %s; create tables without the cyclic constraints, then add those constraints in a manual post-step",
			databaseName,
			strings.Join(cyclic, ", "),
		)
	}
	return ordered, nil
}

func sortTableNamesByDependencies(tableNames []string, dependencies map[string]map[string]struct{}) []string {
	ordered, _ := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	return ordered
}

func sortTableNamesByDependenciesDetailed(tableNames []string, dependencies map[string]map[string]struct{}) ([]string, []string) {
	remainingDependencies := make(map[string]map[string]struct{}, len(tableNames))
	dependents := make(map[string][]string, len(tableNames))
	for _, tableName := range tableNames {
		remainingDependencies[tableName] = map[string]struct{}{}
		for dep := range dependencies[tableName] {
			remainingDependencies[tableName][dep] = struct{}{}
			dependents[dep] = append(dependents[dep], tableName)
		}
	}
	for tableName := range dependents {
		sort.Strings(dependents[tableName])
	}

	ready := make([]string, 0, len(tableNames))
	for _, tableName := range tableNames {
		if len(remainingDependencies[tableName]) == 0 {
			ready = append(ready, tableName)
		}
	}
	sort.Strings(ready)

	ordered := make([]string, 0, len(tableNames))
	processed := make(map[string]struct{}, len(tableNames))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		if _, seen := processed[current]; seen {
			continue
		}
		processed[current] = struct{}{}
		ordered = append(ordered, current)

		for _, dependentTable := range dependents[current] {
			delete(remainingDependencies[dependentTable], current)
			if len(remainingDependencies[dependentTable]) == 0 {
				ready = append(ready, dependentTable)
			}
		}
		sort.Strings(ready)
	}

	if len(ordered) == len(tableNames) {
		return ordered, nil
	}

	remaining := make([]string, 0, len(tableNames)-len(ordered))
	for _, tableName := range tableNames {
		if _, seen := processed[tableName]; !seen {
			remaining = append(remaining, tableName)
		}
	}
	sort.Strings(remaining)
	return append(ordered, remaining...), remaining
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
		sanitized := sanitizeCreateStatementForApply(stmt)
		if _, err := conn.ExecContext(ctx, sanitized); err != nil {
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

func rewriteSchemaStatementForSandbox(statement string, sourceDatabase string, sandboxDatabase string) string {
	if strings.TrimSpace(statement) == "" {
		return statement
	}
	sourceQualified := quoteIdentifier(sourceDatabase) + "."
	sandboxQualified := quoteIdentifier(sandboxDatabase) + "."
	return rewriteQualifiedDatabaseReference(statement, sourceQualified, sandboxQualified)
}

func sanitizeCreateStatementForApply(statement string) string {
	if strings.TrimSpace(statement) == "" {
		return statement
	}
	return definerClauseRe.ReplaceAllString(statement, "DEFINER=CURRENT_USER")
}

func rewriteQualifiedDatabaseReference(statement string, sourceQualified string, sandboxQualified string) string {
	var out strings.Builder
	out.Grow(len(statement))

	inSingle := false
	inDouble := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(statement); i++ {
		if inLineComment {
			out.WriteByte(statement[i])
			if statement[i] == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			out.WriteByte(statement[i])
			if statement[i] == '*' && i+1 < len(statement) && statement[i+1] == '/' {
				out.WriteByte(statement[i+1])
				i++
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble {
			if statement[i] == '#' {
				out.WriteByte(statement[i])
				inLineComment = true
				continue
			}
			if statement[i] == '-' && i+2 < len(statement) && statement[i+1] == '-' && (statement[i+2] == ' ' || statement[i+2] == '\t' || statement[i+2] == '\n') {
				out.WriteString("--")
				i++
				inLineComment = true
				continue
			}
			if statement[i] == '/' && i+1 < len(statement) && statement[i+1] == '*' {
				out.WriteString("/*")
				i++
				inBlockComment = true
				continue
			}
			if strings.HasPrefix(statement[i:], sourceQualified) {
				out.WriteString(sandboxQualified)
				i += len(sourceQualified) - 1
				continue
			}
		}
		if statement[i] == '\'' && !inDouble {
			inSingle = !inSingle
		} else if statement[i] == '"' && !inSingle {
			inDouble = !inDouble
		}
		out.WriteByte(statement[i])
	}

	return out.String()
}
