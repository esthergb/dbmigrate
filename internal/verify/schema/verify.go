package schema

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	baseSchema "github.com/esthergb/dbmigrate/internal/schema"
)

var (
	whitespaceRe    = regexp.MustCompile(`\s+`)
	autoIncrementRe = regexp.MustCompile(`(?i)\bAUTO_INCREMENT\s*=\s*\d+\b`)
	definerRe       = regexp.MustCompile("(?i)\\bDEFINER\\s*=\\s*`[^`]+`@`[^`]+`")
)

const (
	objectTypeTable = "table"
	objectTypeView  = "view"

	diffKindMissingInDestination = "missing_in_destination"
	diffKindMissingInSource      = "missing_in_source"
	diffKindDefinitionMismatch   = "definition_mismatch"
)

// Options controls schema verification behavior.
type Options struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	IncludeTables    bool
	IncludeViews     bool
}

// Diff describes one schema-level mismatch.
type Diff struct {
	Kind            string `json:"kind"`
	Database        string `json:"database"`
	ObjectType      string `json:"object_type"`
	ObjectName      string `json:"object_name"`
	SourceCreateSQL string `json:"source_create_sql,omitempty"`
	DestCreateSQL   string `json:"dest_create_sql,omitempty"`
}

// Summary captures schema verification results.
type Summary struct {
	Databases            int    `json:"databases"`
	ObjectsCompared      int    `json:"objects_compared"`
	MissingInDestination int    `json:"missing_in_destination"`
	MissingInSource      int    `json:"missing_in_source"`
	DefinitionMismatches int    `json:"definition_mismatches"`
	Diffs                []Diff `json:"diffs"`
}

type objectDef struct {
	ObjectType string
	ObjectName string
	CreateSQL  string
	normalized string
}

// Verify compares source and destination schema for selected databases/objects.
func Verify(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	if !opts.IncludeTables && !opts.IncludeViews {
		return Summary{}, errors.New("at least one object type must be enabled (tables or views)")
	}

	sourceDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return Summary{}, fmt.Errorf("list source databases: %w", err)
	}
	destDatabases, err := listDatabases(ctx, dest)
	if err != nil {
		return Summary{}, fmt.Errorf("list destination databases: %w", err)
	}

	unionDatabases := unionAndSort(sourceDatabases, destDatabases)
	selectedDatabases := baseSchema.SelectDatabases(unionDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := Summary{Databases: len(selectedDatabases)}
	for _, databaseName := range selectedDatabases {
		sourceObjects, err := listObjectDefinitions(ctx, source, databaseName, opts.IncludeTables, opts.IncludeViews)
		if err != nil {
			return summary, fmt.Errorf("inspect source schema for %s: %w", databaseName, err)
		}
		destObjects, err := listObjectDefinitions(ctx, dest, databaseName, opts.IncludeTables, opts.IncludeViews)
		if err != nil {
			return summary, fmt.Errorf("inspect destination schema for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches := diffObjectMaps(databaseName, sourceObjects, destObjects)
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.ObjectsCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.DefinitionMismatches += mismatches
	}

	sortDiffs(summary.Diffs)
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

	out := []string{}
	for rows.Next() {
		var schemaName string
		if err := rows.Scan(&schemaName); err != nil {
			return nil, err
		}
		out = append(out, schemaName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func listObjectDefinitions(ctx context.Context, db *sql.DB, databaseName string, includeTables bool, includeViews bool) (map[string]objectDef, error) {
	out := map[string]objectDef{}

	if includeTables {
		names, err := listObjectsByType(ctx, db, databaseName, "BASE TABLE")
		if err != nil {
			return nil, err
		}
		for _, objectName := range names {
			query := fmt.Sprintf("SHOW CREATE TABLE %s.%s", quoteIdentifier(databaseName), quoteIdentifier(objectName))
			createSQL, err := fetchShowCreate(ctx, db, query)
			if err != nil {
				return nil, err
			}
			key := objectTypeTable + ":" + objectName
			out[key] = objectDef{
				ObjectType: objectTypeTable,
				ObjectName: objectName,
				CreateSQL:  createSQL,
				normalized: normalizeCreateStatement(createSQL),
			}
		}
	}

	if includeViews {
		names, err := listObjectsByType(ctx, db, databaseName, "VIEW")
		if err != nil {
			return nil, err
		}
		for _, objectName := range names {
			query := fmt.Sprintf("SHOW CREATE VIEW %s.%s", quoteIdentifier(databaseName), quoteIdentifier(objectName))
			createSQL, err := fetchShowCreate(ctx, db, query)
			if err != nil {
				return nil, err
			}
			key := objectTypeView + ":" + objectName
			out[key] = objectDef{
				ObjectType: objectTypeView,
				ObjectName: objectName,
				CreateSQL:  createSQL,
				normalized: normalizeCreateStatement(createSQL),
			}
		}
	}

	return out, nil
}

func listObjectsByType(ctx context.Context, db *sql.DB, databaseName string, objectType string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
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

func fetchShowCreate(ctx context.Context, db *sql.DB, query string) (string, error) {
	rows, err := db.QueryContext(ctx, query)
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

	createSQL := strings.TrimSpace(string(values[1]))
	if createSQL == "" {
		return "", errors.New("empty CREATE statement")
	}
	return createSQL, nil
}

func diffObjectMaps(databaseName string, source map[string]objectDef, dest map[string]objectDef) ([]Diff, int, int, int, int) {
	diffs := []Diff{}
	compared := 0
	missingDestination := 0
	missingSource := 0
	mismatch := 0

	for key, sourceDef := range source {
		destDef, ok := dest[key]
		if !ok {
			diffs = append(diffs, Diff{
				Kind:            diffKindMissingInDestination,
				Database:        databaseName,
				ObjectType:      sourceDef.ObjectType,
				ObjectName:      sourceDef.ObjectName,
				SourceCreateSQL: sourceDef.CreateSQL,
			})
			missingDestination++
			continue
		}

		compared++
		if sourceDef.normalized != destDef.normalized {
			diffs = append(diffs, Diff{
				Kind:            diffKindDefinitionMismatch,
				Database:        databaseName,
				ObjectType:      sourceDef.ObjectType,
				ObjectName:      sourceDef.ObjectName,
				SourceCreateSQL: sourceDef.CreateSQL,
				DestCreateSQL:   destDef.CreateSQL,
			})
			mismatch++
		}
	}

	for key, destDef := range dest {
		if _, ok := source[key]; ok {
			continue
		}
		diffs = append(diffs, Diff{
			Kind:          diffKindMissingInSource,
			Database:      databaseName,
			ObjectType:    destDef.ObjectType,
			ObjectName:    destDef.ObjectName,
			DestCreateSQL: destDef.CreateSQL,
		})
		missingSource++
	}

	return diffs, compared, missingDestination, missingSource, mismatch
}

func normalizeCreateStatement(createSQL string) string {
	normalized := strings.TrimSpace(createSQL)
	normalized = strings.TrimSuffix(normalized, ";")
	normalized = definerRe.ReplaceAllString(normalized, "DEFINER=?")
	normalized = autoIncrementRe.ReplaceAllString(normalized, "AUTO_INCREMENT=?")
	normalized = whitespaceRe.ReplaceAllString(normalized, " ")
	normalized = foldUnquotedSQLCase(strings.TrimSpace(normalized))
	normalized = strings.ReplaceAll(normalized, "`", "")
	return normalized
}

func foldUnquotedSQLCase(in string) string {
	var out strings.Builder
	out.Grow(len(in))

	inSingle := false
	inDouble := false
	inBacktick := false
	escapeNext := false

	for _, r := range in {
		switch {
		case escapeNext:
			out.WriteRune(r)
			escapeNext = false
			continue
		case r == '\\' && (inSingle || inDouble):
			out.WriteRune(r)
			escapeNext = true
			continue
		case r == '\'' && !inDouble && !inBacktick:
			inSingle = !inSingle
			out.WriteRune(r)
			continue
		case r == '"' && !inSingle && !inBacktick:
			inDouble = !inDouble
			out.WriteRune(r)
			continue
		case r == '`' && !inSingle && !inDouble:
			inBacktick = !inBacktick
			out.WriteRune(r)
			continue
		case inSingle || inDouble || inBacktick:
			out.WriteRune(r)
		default:
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

func sortDiffs(diffs []Diff) {
	sort.Slice(diffs, func(i int, j int) bool {
		left := diffs[i]
		right := diffs[j]
		if left.Database != right.Database {
			return left.Database < right.Database
		}
		if left.ObjectType != right.ObjectType {
			return left.ObjectType < right.ObjectType
		}
		if left.ObjectName != right.ObjectName {
			return left.ObjectName < right.ObjectName
		}
		return left.Kind < right.Kind
	})
}

func unionAndSort(left []string, right []string) []string {
	seen := map[string]struct{}{}
	for _, item := range left {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}
	for _, item := range right {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		seen[trimmed] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for item := range seen {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
