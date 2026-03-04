package data

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	baseSchema "github.com/esthergb/dbmigrate/internal/schema"
)

const (
	diffKindMissingInDestination = "missing_in_destination"
	diffKindMissingInSource      = "missing_in_source"
	diffKindRowCountMismatch     = "row_count_mismatch"
	diffKindTableHashMismatch    = "table_hash_mismatch"
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
	SourceHash  string `json:"source_hash,omitempty"`
	DestHash    string `json:"dest_hash,omitempty"`
}

// Summary captures data verification results.
type Summary struct {
	Databases            int    `json:"databases"`
	TablesCompared       int    `json:"tables_compared"`
	MissingInDestination int    `json:"missing_in_destination"`
	MissingInSource      int    `json:"missing_in_source"`
	CountMismatches      int    `json:"count_mismatches"`
	HashMismatches       int    `json:"hash_mismatches"`
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

// VerifyHash compares deterministic per-table content hashes between source and destination.
func VerifyHash(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (Summary, error) {
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
		sourceHashes, err := tableHashesForDatabase(ctx, source, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read source table hashes for %s: %w", databaseName, err)
		}
		destHashes, err := tableHashesForDatabase(ctx, dest, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read destination table hashes for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches := diffTableHashes(databaseName, sourceHashes, destHashes)
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.TablesCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.HashMismatches += mismatches
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

func tableHashesForDatabase(ctx context.Context, db *sql.DB, databaseName string) (map[string]string, error) {
	tableNames, err := listBaseTables(ctx, db, databaseName)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, tableName := range tableNames {
		tableHash, err := hashTable(ctx, db, databaseName, tableName)
		if err != nil {
			return nil, err
		}
		out[tableName] = tableHash
	}
	return out, nil
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

func listTableColumns(ctx context.Context, db *sql.DB, databaseName string, tableName string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	columns := []string{}
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func countRows(ctx context.Context, db *sql.DB, databaseName string, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
	var out int64
	if err := db.QueryRowContext(ctx, query).Scan(&out); err != nil {
		return 0, err
	}
	return out, nil
}

func hashTable(ctx context.Context, db *sql.DB, databaseName string, tableName string) (string, error) {
	columns, err := listTableColumns(ctx, db, databaseName, tableName)
	if err != nil {
		return "", err
	}
	if len(columns) == 0 {
		empty := sha256.Sum256(nil)
		return hex.EncodeToString(empty[:]), nil
	}

	query := buildOrderedSelectSQL(databaseName, tableName, columns)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	hasher := sha256.New()
	for rows.Next() {
		rowValues := make([]any, len(columns))
		scanValues := make([]any, len(columns))
		for i := range rowValues {
			scanValues[i] = &rowValues[i]
		}
		if err := rows.Scan(scanValues...); err != nil {
			return "", err
		}

		normalized := make([]string, len(rowValues))
		for i, value := range rowValues {
			normalized[i] = normalizeHashValue(value)
		}
		raw, err := json.Marshal(normalized)
		if err != nil {
			return "", err
		}
		if _, err := hasher.Write(raw); err != nil {
			return "", err
		}
		if _, err := hasher.Write([]byte{'\n'}); err != nil {
			return "", err
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func buildOrderedSelectSQL(databaseName string, tableName string, columns []string) string {
	quotedColumns := make([]string, 0, len(columns))
	for _, columnName := range columns {
		quotedColumns = append(quotedColumns, quoteIdentifier(columnName))
	}
	columnList := strings.Join(quotedColumns, ", ")
	return fmt.Sprintf(
		"SELECT %s FROM %s.%s ORDER BY %s",
		columnList,
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
		columnList,
	)
}

func normalizeHashValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null:"
	case []byte:
		return "bytes:" + base64.StdEncoding.EncodeToString(typed)
	case string:
		return "string:" + typed
	case bool:
		return "bool:" + strconv.FormatBool(typed)
	case int:
		return "int:" + strconv.FormatInt(int64(typed), 10)
	case int8:
		return "int8:" + strconv.FormatInt(int64(typed), 10)
	case int16:
		return "int16:" + strconv.FormatInt(int64(typed), 10)
	case int32:
		return "int32:" + strconv.FormatInt(int64(typed), 10)
	case int64:
		return "int64:" + strconv.FormatInt(typed, 10)
	case uint:
		return "uint:" + strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return "uint8:" + strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return "uint16:" + strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return "uint32:" + strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return "uint64:" + strconv.FormatUint(typed, 10)
	case float32:
		return "float32:" + strconv.FormatFloat(float64(typed), 'g', -1, 32)
	case float64:
		return "float64:" + strconv.FormatFloat(typed, 'g', -1, 64)
	case time.Time:
		return "time:" + typed.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprintf("%T:%v", value, value)
	}
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

func diffTableHashes(databaseName string, source map[string]string, dest map[string]string) ([]Diff, int, int, int, int) {
	diffs := []Diff{}
	compared := 0
	missingDestination := 0
	missingSource := 0
	hashMismatch := 0

	for tableName, sourceHash := range source {
		destHash, ok := dest[tableName]
		if !ok {
			diffs = append(diffs, Diff{
				Kind:       diffKindMissingInDestination,
				Database:   databaseName,
				Table:      tableName,
				SourceHash: sourceHash,
			})
			missingDestination++
			continue
		}

		compared++
		if sourceHash != destHash {
			diffs = append(diffs, Diff{
				Kind:       diffKindTableHashMismatch,
				Database:   databaseName,
				Table:      tableName,
				SourceHash: sourceHash,
				DestHash:   destHash,
			})
			hashMismatch++
		}
	}

	for tableName, destHash := range dest {
		if _, ok := source[tableName]; ok {
			continue
		}
		diffs = append(diffs, Diff{
			Kind:     diffKindMissingInSource,
			Database: databaseName,
			Table:    tableName,
			DestHash: destHash,
		})
		missingSource++
	}

	return diffs, compared, missingDestination, missingSource, hashMismatch
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
