package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"

	baseSchema "github.com/esthergb/dbmigrate/internal/schema"
)

const (
	diffKindMissingInDestination = "missing_in_destination"
	diffKindMissingInSource      = "missing_in_source"
	diffKindRowCountMismatch     = "row_count_mismatch"
)

// Options controls data verification behavior.
type Options struct {
	IncludeDatabases []string
	ExcludeDatabases []string
}

// Diff describes one data-level mismatch.
type Diff struct {
	Kind        string `json:"kind"`
	Database    string `json:"database"`
	Table       string `json:"table"`
	SourceCount int64  `json:"source_count,omitempty"`
	DestCount   int64  `json:"dest_count,omitempty"`
}

// Summary captures data verification results.
type Summary struct {
	Databases            int    `json:"databases"`
	TablesCompared       int    `json:"tables_compared"`
	MissingInDestination int    `json:"missing_in_destination"`
	MissingInSource      int    `json:"missing_in_source"`
	CountMismatches      int    `json:"count_mismatches"`
	Diffs                []Diff `json:"diffs"`
}

// VerifyCount compares table row counts between source and destination.
func VerifyCount(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
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
		sourceCounts, err := tableCountsForDatabase(ctx, source, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read source table counts for %s: %w", databaseName, err)
		}
		destCounts, err := tableCountsForDatabase(ctx, dest, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read destination table counts for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches := diffTableCounts(databaseName, sourceCounts, destCounts)
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.TablesCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.CountMismatches += mismatches
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

func tableCountsForDatabase(ctx context.Context, db *sql.DB, databaseName string) (map[string]int64, error) {
	tableNames, err := listBaseTables(ctx, db, databaseName)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, tableName := range tableNames {
		count, err := countRows(ctx, db, databaseName, tableName)
		if err != nil {
			return nil, err
		}
		counts[tableName] = count
	}
	return counts, nil
}

func listBaseTables(ctx context.Context, db *sql.DB, databaseName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT TABLE_NAME
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`, databaseName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := []string{}
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, err
		}
		out = append(out, tableName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func countRows(ctx context.Context, db *sql.DB, databaseName string, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
	var out int64
	if err := db.QueryRowContext(ctx, query).Scan(&out); err != nil {
		return 0, err
	}
	return out, nil
}

func diffTableCounts(databaseName string, source map[string]int64, dest map[string]int64) ([]Diff, int, int, int, int) {
	diffs := []Diff{}
	compared := 0
	missingDestination := 0
	missingSource := 0
	countMismatch := 0

	for tableName, sourceCount := range source {
		destCount, ok := dest[tableName]
		if !ok {
			diffs = append(diffs, Diff{
				Kind:        diffKindMissingInDestination,
				Database:    databaseName,
				Table:       tableName,
				SourceCount: sourceCount,
			})
			missingDestination++
			continue
		}

		compared++
		if sourceCount != destCount {
			diffs = append(diffs, Diff{
				Kind:        diffKindRowCountMismatch,
				Database:    databaseName,
				Table:       tableName,
				SourceCount: sourceCount,
				DestCount:   destCount,
			})
			countMismatch++
		}
	}

	for tableName, destCount := range dest {
		if _, ok := source[tableName]; ok {
			continue
		}
		diffs = append(diffs, Diff{
			Kind:      diffKindMissingInSource,
			Database:  databaseName,
			Table:     tableName,
			DestCount: destCount,
		})
		missingSource++
	}

	return diffs, compared, missingDestination, missingSource, countMismatch
}

func sortDiffs(diffs []Diff) {
	sort.Slice(diffs, func(i int, j int) bool {
		left := diffs[i]
		right := diffs[j]
		if left.Database != right.Database {
			return left.Database < right.Database
		}
		if left.Table != right.Table {
			return left.Table < right.Table
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
