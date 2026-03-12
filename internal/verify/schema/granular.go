package schema

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	baseSchema "github.com/esthergb/dbmigrate/internal/schema"
)

const (
	granularKindColumnMissing     = "column_missing_in_destination"
	granularKindColumnExtra       = "column_extra_in_destination"
	granularKindColumnMismatch    = "column_definition_mismatch"
	granularKindIndexMissing      = "index_missing_in_destination"
	granularKindIndexExtra        = "index_extra_in_destination"
	granularKindIndexMismatch     = "index_definition_mismatch"
	granularKindFKMissing         = "fk_missing_in_destination"
	granularKindFKExtra           = "fk_extra_in_destination"
	granularKindFKMismatch        = "fk_definition_mismatch"
	granularKindPartitionMissing  = "partition_missing_in_destination"
	granularKindPartitionExtra    = "partition_extra_in_destination"
	granularKindPartitionMismatch = "partition_definition_mismatch"
)

// GranularDiff describes a single column-, index-, FK-, or partition-level difference.
type GranularDiff struct {
	Kind       string `json:"kind"`
	Database   string `json:"database"`
	Table      string `json:"table"`
	ObjectName string `json:"object_name"`
	SourceDef  string `json:"source_def,omitempty"`
	DestDef    string `json:"dest_def,omitempty"`
}

// GranularSummary captures granular schema verification results.
type GranularSummary struct {
	Databases      int            `json:"databases"`
	TablesCompared int            `json:"tables_compared"`
	ColumnDiffs    int            `json:"column_diffs"`
	IndexDiffs     int            `json:"index_diffs"`
	FKDiffs        int            `json:"fk_diffs"`
	PartitionDiffs int            `json:"partition_diffs"`
	Diffs          []GranularDiff `json:"diffs"`
}

// VerifyGranular performs column-, index-, FK-, and partition-level schema comparison.
func VerifyGranular(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (GranularSummary, error) {
	if source == nil || dest == nil {
		return GranularSummary{}, fmt.Errorf("source and destination connections are required")
	}
	if !opts.IncludeTables {
		return GranularSummary{}, fmt.Errorf("granular schema verification requires tables in --include-objects")
	}

	sourceDatabases, err := listDatabases(ctx, source)
	if err != nil {
		return GranularSummary{}, fmt.Errorf("list source databases: %w", err)
	}
	destDatabases, err := listDatabases(ctx, dest)
	if err != nil {
		return GranularSummary{}, fmt.Errorf("list destination databases: %w", err)
	}

	unionDatabases := unionAndSort(sourceDatabases, destDatabases)
	selectedDatabases := baseSchema.SelectDatabases(unionDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := GranularSummary{Databases: len(selectedDatabases)}

	for _, db := range selectedDatabases {
		srcTables, err := listObjectsByType(ctx, source, db, "BASE TABLE")
		if err != nil {
			return summary, fmt.Errorf("list source tables for %s: %w", db, err)
		}
		dstTables, err := listObjectsByType(ctx, dest, db, "BASE TABLE")
		if err != nil {
			return summary, fmt.Errorf("list dest tables for %s: %w", db, err)
		}

		srcSet := toSet(srcTables)
		dstSet := toSet(dstTables)
		allTables := unionStrings(srcTables, dstTables)

		for _, table := range allTables {
			_, inSrc := srcSet[table]
			_, inDst := dstSet[table]
			if !inSrc || !inDst {
				continue
			}

			summary.TablesCompared++

			colDiffs, err := diffColumns(ctx, source, dest, db, table)
			if err != nil {
				return summary, fmt.Errorf("diff columns for %s.%s: %w", db, table, err)
			}
			summary.Diffs = append(summary.Diffs, colDiffs...)
			summary.ColumnDiffs += len(colDiffs)

			idxDiffs, err := diffIndexes(ctx, source, dest, db, table)
			if err != nil {
				return summary, fmt.Errorf("diff indexes for %s.%s: %w", db, table, err)
			}
			summary.Diffs = append(summary.Diffs, idxDiffs...)
			summary.IndexDiffs += len(idxDiffs)

			fkDiffs, err := diffForeignKeys(ctx, source, dest, db, table)
			if err != nil {
				return summary, fmt.Errorf("diff foreign keys for %s.%s: %w", db, table, err)
			}
			summary.Diffs = append(summary.Diffs, fkDiffs...)
			summary.FKDiffs += len(fkDiffs)

			partDiffs, err := diffPartitions(ctx, source, dest, db, table)
			if err != nil {
				return summary, fmt.Errorf("diff partitions for %s.%s: %w", db, table, err)
			}
			summary.Diffs = append(summary.Diffs, partDiffs...)
			summary.PartitionDiffs += len(partDiffs)
		}
	}

	sortGranularDiffs(summary.Diffs)
	return summary, nil
}

type columnDef struct {
	name       string
	colType    string
	isNullable string
	colDefault sql.NullString
	extra      string
	charSet    sql.NullString
	collation  sql.NullString
}

func (c columnDef) signature() string {
	def := normalizeStr(c.colType)
	def += " nullable=" + c.isNullable
	if c.colDefault.Valid {
		def += " default=" + c.colDefault.String
	}
	if c.extra != "" {
		def += " extra=" + normalizeStr(c.extra)
	}
	if c.charSet.Valid && c.charSet.String != "" {
		def += " charset=" + strings.ToLower(c.charSet.String)
	}
	if c.collation.Valid && c.collation.String != "" {
		def += " collation=" + strings.ToLower(c.collation.String)
	}
	return def
}

func diffColumns(ctx context.Context, srcDB *sql.DB, dstDB *sql.DB, dbName, tableName string) ([]GranularDiff, error) {
	srcCols, err := queryColumns(ctx, srcDB, dbName, tableName)
	if err != nil {
		return nil, err
	}
	dstCols, err := queryColumns(ctx, dstDB, dbName, tableName)
	if err != nil {
		return nil, err
	}

	var diffs []GranularDiff
	srcMap := make(map[string]columnDef, len(srcCols))
	for _, c := range srcCols {
		srcMap[c.name] = c
	}
	dstMap := make(map[string]columnDef, len(dstCols))
	for _, c := range dstCols {
		dstMap[c.name] = c
	}

	for _, sc := range srcCols {
		dc, ok := dstMap[sc.name]
		if !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindColumnMissing, Database: dbName, Table: tableName,
				ObjectName: sc.name, SourceDef: sc.signature(),
			})
			continue
		}
		if sc.signature() != dc.signature() {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindColumnMismatch, Database: dbName, Table: tableName,
				ObjectName: sc.name, SourceDef: sc.signature(), DestDef: dc.signature(),
			})
		}
	}
	for _, dc := range dstCols {
		if _, ok := srcMap[dc.name]; !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindColumnExtra, Database: dbName, Table: tableName,
				ObjectName: dc.name, DestDef: dc.signature(),
			})
		}
	}
	return diffs, nil
}

func queryColumns(ctx context.Context, db *sql.DB, dbName, tableName string) ([]columnDef, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA,
		       CHARACTER_SET_NAME, COLLATION_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []columnDef
	for rows.Next() {
		var c columnDef
		if err := rows.Scan(&c.name, &c.colType, &c.isNullable, &c.colDefault, &c.extra, &c.charSet, &c.collation); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type indexDef struct {
	name      string
	nonUnique int
	seqInIdx  int
	colName   string
	indexType string
}

func diffIndexes(ctx context.Context, srcDB *sql.DB, dstDB *sql.DB, dbName, tableName string) ([]GranularDiff, error) {
	srcIdxs, err := queryIndexSignatures(ctx, srcDB, dbName, tableName)
	if err != nil {
		return nil, err
	}
	dstIdxs, err := queryIndexSignatures(ctx, dstDB, dbName, tableName)
	if err != nil {
		return nil, err
	}

	var diffs []GranularDiff
	for name, srcSig := range srcIdxs {
		dstSig, ok := dstIdxs[name]
		if !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindIndexMissing, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig,
			})
			continue
		}
		if srcSig != dstSig {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindIndexMismatch, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig, DestDef: dstSig,
			})
		}
	}
	for name, dstSig := range dstIdxs {
		if _, ok := srcIdxs[name]; !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindIndexExtra, Database: dbName, Table: tableName,
				ObjectName: name, DestDef: dstSig,
			})
		}
	}
	return diffs, nil
}

func queryIndexSignatures(ctx context.Context, db *sql.DB, dbName, tableName string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, INDEX_TYPE
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type part struct {
		nonUnique int
		colName   string
		indexType string
	}
	byName := map[string][]part{}
	for rows.Next() {
		var d indexDef
		if err := rows.Scan(&d.name, &d.nonUnique, &d.seqInIdx, &d.colName, &d.indexType); err != nil {
			return nil, err
		}
		byName[d.name] = append(byName[d.name], part{d.nonUnique, d.colName, d.indexType})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sigs := make(map[string]string, len(byName))
	for name, parts := range byName {
		cols := make([]string, 0, len(parts))
		for _, p := range parts {
			cols = append(cols, p.colName)
		}
		unique := "unique"
		if parts[0].nonUnique != 0 {
			unique = "non_unique"
		}
		sigs[name] = fmt.Sprintf("type=%s unique=%s cols=%s", strings.ToLower(parts[0].indexType), unique, strings.Join(cols, ","))
	}
	return sigs, nil
}

type fkDef struct {
	name       string
	colName    string
	refTable   string
	refCol     string
	updateRule string
	deleteRule string
}

func diffForeignKeys(ctx context.Context, srcDB *sql.DB, dstDB *sql.DB, dbName, tableName string) ([]GranularDiff, error) {
	srcFKs, err := queryFKSignatures(ctx, srcDB, dbName, tableName)
	if err != nil {
		return nil, err
	}
	dstFKs, err := queryFKSignatures(ctx, dstDB, dbName, tableName)
	if err != nil {
		return nil, err
	}

	var diffs []GranularDiff
	for name, srcSig := range srcFKs {
		dstSig, ok := dstFKs[name]
		if !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindFKMissing, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig,
			})
			continue
		}
		if srcSig != dstSig {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindFKMismatch, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig, DestDef: dstSig,
			})
		}
	}
	for name, dstSig := range dstFKs {
		if _, ok := srcFKs[name]; !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindFKExtra, Database: dbName, Table: tableName,
				ObjectName: name, DestDef: dstSig,
			})
		}
	}
	return diffs, nil
}

func queryFKSignatures(ctx context.Context, db *sql.DB, dbName, tableName string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT rc.CONSTRAINT_NAME,
		       kcu.COLUMN_NAME,
		       kcu.REFERENCED_TABLE_NAME,
		       kcu.REFERENCED_COLUMN_NAME,
		       rc.UPDATE_RULE,
		       rc.DELETE_RULE
		FROM information_schema.REFERENTIAL_CONSTRAINTS rc
		JOIN information_schema.KEY_COLUMN_USAGE kcu
		  ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		 AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
		 AND kcu.TABLE_NAME = ?
		WHERE rc.CONSTRAINT_SCHEMA = ?
		ORDER BY rc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION
	`, tableName, dbName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type part struct {
		col    string
		refTab string
		refCol string
		upd    string
		del    string
	}
	byName := map[string][]part{}
	for rows.Next() {
		var d fkDef
		if err := rows.Scan(&d.name, &d.colName, &d.refTable, &d.refCol, &d.updateRule, &d.deleteRule); err != nil {
			return nil, err
		}
		byName[d.name] = append(byName[d.name], part{d.colName, d.refTable, d.refCol, d.updateRule, d.deleteRule})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sigs := make(map[string]string, len(byName))
	for name, parts := range byName {
		cols := make([]string, 0, len(parts))
		refCols := make([]string, 0, len(parts))
		for _, p := range parts {
			cols = append(cols, p.col)
			refCols = append(refCols, p.refCol)
		}
		sigs[name] = fmt.Sprintf("ref=%s(%s) on_update=%s on_delete=%s cols=%s",
			parts[0].refTab, strings.Join(refCols, ","),
			strings.ToUpper(parts[0].upd), strings.ToUpper(parts[0].del),
			strings.Join(cols, ","))
	}
	return sigs, nil
}

func diffPartitions(ctx context.Context, srcDB *sql.DB, dstDB *sql.DB, dbName, tableName string) ([]GranularDiff, error) {
	srcParts, err := queryPartitionSignatures(ctx, srcDB, dbName, tableName)
	if err != nil {
		return nil, err
	}
	dstParts, err := queryPartitionSignatures(ctx, dstDB, dbName, tableName)
	if err != nil {
		return nil, err
	}

	if len(srcParts) == 0 && len(dstParts) == 0 {
		return nil, nil
	}

	var diffs []GranularDiff
	for name, srcSig := range srcParts {
		dstSig, ok := dstParts[name]
		if !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindPartitionMissing, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig,
			})
			continue
		}
		if srcSig != dstSig {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindPartitionMismatch, Database: dbName, Table: tableName,
				ObjectName: name, SourceDef: srcSig, DestDef: dstSig,
			})
		}
	}
	for name, dstSig := range dstParts {
		if _, ok := srcParts[name]; !ok {
			diffs = append(diffs, GranularDiff{
				Kind: granularKindPartitionExtra, Database: dbName, Table: tableName,
				ObjectName: name, DestDef: dstSig,
			})
		}
	}
	return diffs, nil
}

func queryPartitionSignatures(ctx context.Context, db *sql.DB, dbName, tableName string) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT PARTITION_NAME, PARTITION_METHOD, PARTITION_EXPRESSION,
		       PARTITION_DESCRIPTION, PARTITION_ORDINAL_POSITION
		FROM information_schema.PARTITIONS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		  AND PARTITION_NAME IS NOT NULL
		ORDER BY PARTITION_ORDINAL_POSITION
	`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	sigs := map[string]string{}
	for rows.Next() {
		var name, method string
		var expr, desc sql.NullString
		var pos int
		if err := rows.Scan(&name, &method, &expr, &desc, &pos); err != nil {
			return nil, err
		}
		sig := fmt.Sprintf("method=%s expr=%s desc=%s pos=%d",
			strings.ToUpper(method),
			expr.String,
			desc.String,
			pos,
		)
		sigs[name] = sig
	}
	return sigs, rows.Err()
}

func sortGranularDiffs(diffs []GranularDiff) {
	sort.Slice(diffs, func(i, j int) bool {
		l, r := diffs[i], diffs[j]
		if l.Database != r.Database {
			return l.Database < r.Database
		}
		if l.Table != r.Table {
			return l.Table < r.Table
		}
		if l.Kind != r.Kind {
			return l.Kind < r.Kind
		}
		return l.ObjectName < r.ObjectName
	})
}

func normalizeStr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func toSet(items []string) map[string]struct{} {
	m := make(map[string]struct{}, len(items))
	for _, s := range items {
		m[s] = struct{}{}
	}
	return m
}

func unionStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
