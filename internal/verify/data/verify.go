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

const (
	defaultHashChunkSize     = 2000
	defaultFullHashChunkSize = 500
	defaultSampleSize        = 1000
)

type hashMode string

const (
	hashModeSample   hashMode = "sample"
	hashModeHash     hashMode = "hash"
	hashModeFullHash hashMode = "full-hash"
)

type sqlQueryer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Options controls data verification behavior.
type Options struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	SampleSize       int
}

// Diff describes one data-level mismatch.
type Diff struct {
	Kind        string   `json:"kind"`
	Database    string   `json:"database"`
	Table       string   `json:"table"`
	SourceCount int64    `json:"source_count,omitempty"`
	DestCount   int64    `json:"dest_count,omitempty"`
	SourceHash  string   `json:"source_hash,omitempty"`
	DestHash    string   `json:"dest_hash,omitempty"`
	NoiseRisk   string   `json:"noise_risk,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

type CanonicalizationSummary struct {
	RowOrderIndependent bool   `json:"row_order_independent"`
	SessionTimeZone     string `json:"session_time_zone"`
	TextByteNormalized  bool   `json:"text_byte_normalized"`
	JSONNormalized      bool   `json:"json_normalized"`
	SampleFullScan      bool   `json:"sample_full_scan"`
	AggregateStrategy   string `json:"aggregate_strategy"`
}

type TableRisk struct {
	Database                  string   `json:"database"`
	Table                     string   `json:"table"`
	ApproximateNumericColumns int      `json:"approximate_numeric_columns,omitempty"`
	TemporalColumns           int      `json:"temporal_columns,omitempty"`
	JSONColumns               int      `json:"json_columns,omitempty"`
	CollationSensitiveColumns int      `json:"collation_sensitive_columns,omitempty"`
	Notes                     []string `json:"notes,omitempty"`
}

// Summary captures data verification results.
type Summary struct {
	Databases                int                     `json:"databases"`
	TablesCompared           int                     `json:"tables_compared"`
	MissingInDestination     int                     `json:"missing_in_destination"`
	MissingInSource          int                     `json:"missing_in_source"`
	CountMismatches          int                     `json:"count_mismatches"`
	HashMismatches           int                     `json:"hash_mismatches"`
	NoiseRiskMismatches      int                     `json:"noise_risk_mismatches,omitempty"`
	RepresentationRiskTables int                     `json:"representation_risk_tables,omitempty"`
	Canonicalization         CanonicalizationSummary `json:"canonicalization,omitempty"`
	TableRisks               []TableRisk             `json:"table_risks,omitempty"`
	Diffs                    []Diff                  `json:"diffs"`
}

type columnInfo struct {
	Name             string
	DataType         string
	CollationName    string
	CharacterSetName string
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

	sourceConn, err := source.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin source verify connection: %w", err)
	}
	defer func() {
		_ = sourceConn.Close()
	}()
	destConn, err := dest.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin destination verify connection: %w", err)
	}
	defer func() {
		_ = destConn.Close()
	}()

	sourceDatabases, err := listDatabases(ctx, sourceConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list source databases: %w", err)
	}
	destDatabases, err := listDatabases(ctx, destConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list destination databases: %w", err)
	}

	unionDatabases := unionAndSort(sourceDatabases, destDatabases)
	selectedDatabases := baseSchema.SelectDatabases(unionDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := Summary{
		Databases: len(selectedDatabases),
		Canonicalization: CanonicalizationSummary{
			RowOrderIndependent: true,
			SessionTimeZone:     "+00:00",
			TextByteNormalized:  true,
			JSONNormalized:      true,
			SampleFullScan:      false,
			AggregateStrategy:   "ordered_key_chunked_streaming_sha256",
		},
	}
	if err := prepareVerifySession(ctx, sourceConn); err != nil {
		return summary, fmt.Errorf("prepare source verify session: %w", err)
	}
	if err := prepareVerifySession(ctx, destConn); err != nil {
		return summary, fmt.Errorf("prepare destination verify session: %w", err)
	}
	riskProfiles, err := tableRiskProfiles(ctx, sourceConn, selectedDatabases)
	if err != nil {
		return summary, fmt.Errorf("read source table risk profiles: %w", err)
	}
	summary.TableRisks = flattenTableRisks(riskProfiles)
	summary.RepresentationRiskTables = len(summary.TableRisks)
	for _, databaseName := range selectedDatabases {
		sourceHashes, err := tableHashesForDatabase(ctx, sourceConn, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read source table hashes for %s: %w", databaseName, err)
		}
		destHashes, err := tableHashesForDatabase(ctx, destConn, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read destination table hashes for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches, noiseRisk := diffTableHashes(databaseName, sourceHashes, destHashes, riskProfiles[databaseName])
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.TablesCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.HashMismatches += mismatches
		summary.NoiseRiskMismatches += noiseRisk
	}

	sortDiffs(summary.Diffs)
	return summary, nil
}

// VerifyFullHash compares full-table deterministic content hashes for all selected tables.
func VerifyFullHash(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}

	sourceConn, err := source.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin source verify connection: %w", err)
	}
	defer func() {
		_ = sourceConn.Close()
	}()
	destConn, err := dest.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin destination verify connection: %w", err)
	}
	defer func() {
		_ = destConn.Close()
	}()

	sourceDatabases, err := listDatabases(ctx, sourceConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list source databases: %w", err)
	}
	destDatabases, err := listDatabases(ctx, destConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list destination databases: %w", err)
	}

	unionDatabases := unionAndSort(sourceDatabases, destDatabases)
	selectedDatabases := baseSchema.SelectDatabases(unionDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := Summary{
		Databases: len(selectedDatabases),
		Canonicalization: CanonicalizationSummary{
			RowOrderIndependent: true,
			SessionTimeZone:     "+00:00",
			TextByteNormalized:  true,
			JSONNormalized:      true,
			SampleFullScan:      false,
			AggregateStrategy:   "ordered_key_chunked_fullhash_sha256",
		},
	}
	if err := prepareVerifySession(ctx, sourceConn); err != nil {
		return summary, fmt.Errorf("prepare source verify session: %w", err)
	}
	if err := prepareVerifySession(ctx, destConn); err != nil {
		return summary, fmt.Errorf("prepare destination verify session: %w", err)
	}
	riskProfiles, err := tableRiskProfiles(ctx, sourceConn, selectedDatabases)
	if err != nil {
		return summary, fmt.Errorf("read source table risk profiles: %w", err)
	}
	summary.TableRisks = flattenTableRisks(riskProfiles)
	summary.RepresentationRiskTables = len(summary.TableRisks)
	for _, databaseName := range selectedDatabases {
		sourceHashes, err := tableFullHashesForDatabase(ctx, sourceConn, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read source table full hashes for %s: %w", databaseName, err)
		}
		destHashes, err := tableFullHashesForDatabase(ctx, destConn, databaseName)
		if err != nil {
			return summary, fmt.Errorf("read destination table full hashes for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches, noiseRisk := diffTableHashes(databaseName, sourceHashes, destHashes, riskProfiles[databaseName])
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.TablesCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.HashMismatches += mismatches
		summary.NoiseRiskMismatches += noiseRisk
	}

	sortDiffs(summary.Diffs)
	return summary, nil
}

// VerifySample compares deterministic per-table sample hashes between source and destination.
func VerifySample(ctx context.Context, source *sql.DB, dest *sql.DB, opts Options) (Summary, error) {
	if source == nil || dest == nil {
		return Summary{}, errors.New("source and destination connections are required")
	}
	if opts.SampleSize < 1 {
		opts.SampleSize = 1000
	}

	sourceConn, err := source.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin source verify connection: %w", err)
	}
	defer func() {
		_ = sourceConn.Close()
	}()
	destConn, err := dest.Conn(ctx)
	if err != nil {
		return Summary{}, fmt.Errorf("pin destination verify connection: %w", err)
	}
	defer func() {
		_ = destConn.Close()
	}()

	sourceDatabases, err := listDatabases(ctx, sourceConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list source databases: %w", err)
	}
	destDatabases, err := listDatabases(ctx, destConn)
	if err != nil {
		return Summary{}, fmt.Errorf("list destination databases: %w", err)
	}

	unionDatabases := unionAndSort(sourceDatabases, destDatabases)
	selectedDatabases := baseSchema.SelectDatabases(unionDatabases, opts.IncludeDatabases, opts.ExcludeDatabases)

	summary := Summary{
		Databases: len(selectedDatabases),
		Canonicalization: CanonicalizationSummary{
			RowOrderIndependent: false,
			SessionTimeZone:     "+00:00",
			TextByteNormalized:  true,
			JSONNormalized:      true,
			SampleFullScan:      false,
			AggregateStrategy:   "ordered_key_limited_streaming_sha256",
		},
	}
	if err := prepareVerifySession(ctx, sourceConn); err != nil {
		return summary, fmt.Errorf("prepare source verify session: %w", err)
	}
	if err := prepareVerifySession(ctx, destConn); err != nil {
		return summary, fmt.Errorf("prepare destination verify session: %w", err)
	}
	riskProfiles, err := tableRiskProfiles(ctx, sourceConn, selectedDatabases)
	if err != nil {
		return summary, fmt.Errorf("read source table risk profiles: %w", err)
	}
	summary.TableRisks = flattenTableRisks(riskProfiles)
	summary.RepresentationRiskTables = len(summary.TableRisks)
	for _, databaseName := range selectedDatabases {
		sourceHashes, err := tableSampleHashesForDatabase(ctx, sourceConn, databaseName, opts.SampleSize)
		if err != nil {
			return summary, fmt.Errorf("read source table sample hashes for %s: %w", databaseName, err)
		}
		destHashes, err := tableSampleHashesForDatabase(ctx, destConn, databaseName, opts.SampleSize)
		if err != nil {
			return summary, fmt.Errorf("read destination table sample hashes for %s: %w", databaseName, err)
		}

		diffs, compared, missingDest, missingSource, mismatches, noiseRisk := diffTableHashes(databaseName, sourceHashes, destHashes, riskProfiles[databaseName])
		summary.Diffs = append(summary.Diffs, diffs...)
		summary.TablesCompared += compared
		summary.MissingInDestination += missingDest
		summary.MissingInSource += missingSource
		summary.HashMismatches += mismatches
		summary.NoiseRiskMismatches += noiseRisk
	}

	sortDiffs(summary.Diffs)
	return summary, nil
}

func listDatabases(ctx context.Context, queryer sqlQueryer) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
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

func tableCountsForDatabase(ctx context.Context, queryer sqlQueryer, databaseName string) (map[string]int64, error) {
	tableNames, err := listBaseTables(ctx, queryer, databaseName)
	if err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, tableName := range tableNames {
		count, err := countRows(ctx, queryer, databaseName, tableName)
		if err != nil {
			return nil, err
		}
		counts[tableName] = count
	}
	return counts, nil
}

func tableHashesForDatabase(ctx context.Context, queryer sqlQueryer, databaseName string) (map[string]string, error) {
	return tableModeHashesForDatabase(ctx, queryer, databaseName, hashModeHash, 0)
}

func tableFullHashesForDatabase(ctx context.Context, queryer sqlQueryer, databaseName string) (map[string]string, error) {
	return tableModeHashesForDatabase(ctx, queryer, databaseName, hashModeFullHash, 0)
}

func tableSampleHashesForDatabase(ctx context.Context, queryer sqlQueryer, databaseName string, sampleSize int) (map[string]string, error) {
	return tableModeHashesForDatabase(ctx, queryer, databaseName, hashModeSample, sampleSize)
}

func tableModeHashesForDatabase(ctx context.Context, queryer sqlQueryer, databaseName string, mode hashMode, sampleSize int) (map[string]string, error) {
	tableNames, err := listBaseTables(ctx, queryer, databaseName)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, tableName := range tableNames {
		tableHash, err := hashTableByMode(ctx, queryer, databaseName, tableName, mode, sampleSize)
		if err != nil {
			return nil, err
		}
		out[tableName] = tableHash
	}
	return out, nil
}

func listBaseTables(ctx context.Context, queryer sqlQueryer, databaseName string) ([]string, error) {
	rows, err := queryer.QueryContext(ctx, `
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

func listTableColumnInfo(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) ([]columnInfo, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT COLUMN_NAME, DATA_TYPE, COALESCE(COLLATION_NAME, ''), COALESCE(CHARACTER_SET_NAME, '')
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

	columns := []columnInfo{}
	for rows.Next() {
		var info columnInfo
		if err := rows.Scan(&info.Name, &info.DataType, &info.CollationName, &info.CharacterSetName); err != nil {
			return nil, err
		}
		info.DataType = strings.ToLower(strings.TrimSpace(info.DataType))
		info.CollationName = strings.TrimSpace(info.CollationName)
		info.CharacterSetName = strings.TrimSpace(info.CharacterSetName)
		columns = append(columns, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return columns, nil
}

func countRows(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
	var out int64
	if err := queryer.QueryRowContext(ctx, query).Scan(&out); err != nil {
		return 0, err
	}
	return out, nil
}

func hashTableByMode(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string, mode hashMode, sampleSize int) (string, error) {
	columns, err := listTableColumnInfo(ctx, queryer, databaseName, tableName)
	if err != nil {
		return "", err
	}
	if len(columns) == 0 {
		empty := sha256.Sum256(nil)
		return hex.EncodeToString(empty[:]), nil
	}

	keyColumns, err := listStableKeyColumns(ctx, queryer, databaseName, tableName)
	if err != nil {
		return "", err
	}
	if mode != hashModeSample && len(keyColumns) == 0 {
		return "", incompatibleStableKeyError(databaseName, tableName)
	}

	switch mode {
	case hashModeSample:
		return hashOrderedSampleTable(ctx, queryer, databaseName, tableName, columns, keyColumns, sampleSize)
	case hashModeHash:
		return hashChunkedTable(ctx, queryer, databaseName, tableName, columns, keyColumns, defaultHashChunkSize, hashModeHash)
	case hashModeFullHash:
		return hashChunkedTable(ctx, queryer, databaseName, tableName, columns, keyColumns, defaultFullHashChunkSize, hashModeFullHash)
	default:
		return "", fmt.Errorf("unsupported hash mode %q", mode)
	}
}

func hashOrderedSampleTable(
	ctx context.Context,
	queryer sqlQueryer,
	databaseName string,
	tableName string,
	columns []columnInfo,
	keyColumns []string,
	sampleSize int,
) (string, error) {
	if sampleSize < 1 {
		sampleSize = defaultSampleSize
	}

	query := buildSelectSQL(databaseName, tableName, columns)
	if len(keyColumns) > 0 {
		query = buildOrderedSelectSQL(databaseName, tableName, columns, keyColumns, true)
	} else {
		query = query + " LIMIT ?"
	}

	rows, err := queryer.QueryContext(ctx, query, sampleSize)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = rows.Close()
	}()

	return hashStreamedRows(rows, columns, hashModeSample)
}

func hashChunkedTable(
	ctx context.Context,
	queryer sqlQueryer,
	databaseName string,
	tableName string,
	columns []columnInfo,
	keyColumns []string,
	chunkSize int,
	mode hashMode,
) (string, error) {
	if chunkSize < 1 {
		chunkSize = defaultHashChunkSize
	}

	keyIndexes, err := keyColumnIndexes(columns, keyColumns)
	if err != nil {
		return "", err
	}

	tableHasher := sha256.New()
	cursor := make([]any, 0, len(keyColumns))
	totalRows := 0
	chunks := 0
	for {
		query := buildKeysetSelectSQL(databaseName, tableName, columns, keyColumns, len(cursor) > 0)
		args := make([]any, 0, len(cursor)+1)
		args = append(args, cursor...)
		args = append(args, chunkSize)

		rows, err := queryer.QueryContext(ctx, query, args...)
		if err != nil {
			return "", err
		}

		chunkHasher := sha256.New()
		chunkRows := 0
		lastKey := make([]any, 0, len(keyColumns))
		for rows.Next() {
			rowValues := make([]any, len(columns))
			scanValues := make([]any, len(columns))
			for i := range rowValues {
				scanValues[i] = &rowValues[i]
			}
			if err := rows.Scan(scanValues...); err != nil {
				_ = rows.Close()
				return "", err
			}
			for i, value := range rowValues {
				if raw, ok := value.([]byte); ok {
					copied := make([]byte, len(raw))
					copy(copied, raw)
					rowValues[i] = copied
				}
			}

			rowDigest, err := rowDigestForHash(rowValues, columns)
			if err != nil {
				_ = rows.Close()
				return "", err
			}

			if mode == hashModeHash {
				if _, err := tableHasher.Write([]byte("r:")); err != nil {
					_ = rows.Close()
					return "", err
				}
				if _, err := tableHasher.Write(rowDigest); err != nil {
					_ = rows.Close()
					return "", err
				}
				if _, err := tableHasher.Write([]byte{'\n'}); err != nil {
					_ = rows.Close()
					return "", err
				}
			} else {
				if _, err := chunkHasher.Write(rowDigest); err != nil {
					_ = rows.Close()
					return "", err
				}
				if _, err := chunkHasher.Write([]byte{'\n'}); err != nil {
					_ = rows.Close()
					return "", err
				}
			}

			lastKey = keyCursorFromRow(rowValues, keyIndexes)
			chunkRows++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return "", err
		}
		_ = rows.Close()

		if chunkRows == 0 {
			break
		}
		if mode == hashModeFullHash {
			if _, err := fmt.Fprintf(tableHasher, "chunk:%d rows:%d digest:", chunks, chunkRows); err != nil {
				return "", err
			}
			if _, err := tableHasher.Write(chunkHasher.Sum(nil)); err != nil {
				return "", err
			}
			if _, err := tableHasher.Write([]byte{'\n'}); err != nil {
				return "", err
			}
		}

		totalRows += chunkRows
		chunks++
		cursor = lastKey
		if chunkRows < chunkSize {
			break
		}
	}

	if _, err := fmt.Fprintf(tableHasher, "mode:%s rows:%d chunks:%d\n", mode, totalRows, chunks); err != nil {
		return "", err
	}
	return hex.EncodeToString(tableHasher.Sum(nil)), nil
}

func rowDigestForHash(rowValues []any, columns []columnInfo) ([]byte, error) {
	normalized := make([]string, len(rowValues))
	for i, value := range rowValues {
		normalized[i] = normalizeHashValue(value, columns[i])
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	rowSum := sha256.Sum256(raw)
	return rowSum[:], nil
}

func hashStreamedRows(rows *sql.Rows, columns []columnInfo, mode hashMode) (string, error) {
	hasher := sha256.New()
	rowCount := 0

	for rows.Next() {
		rowValues := make([]any, len(columns))
		scanValues := make([]any, len(columns))
		for i := range rowValues {
			scanValues[i] = &rowValues[i]
		}
		if err := rows.Scan(scanValues...); err != nil {
			return "", err
		}
		for i, value := range rowValues {
			if raw, ok := value.([]byte); ok {
				copied := make([]byte, len(raw))
				copy(copied, raw)
				rowValues[i] = copied
			}
		}

		rowDigest, err := rowDigestForHash(rowValues, columns)
		if err != nil {
			return "", err
		}
		if _, err := hasher.Write([]byte("r:")); err != nil {
			return "", err
		}
		if _, err := hasher.Write(rowDigest); err != nil {
			return "", err
		}
		if _, err := hasher.Write([]byte{'\n'}); err != nil {
			return "", err
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if _, err := fmt.Fprintf(hasher, "mode:%s rows:%d\n", mode, rowCount); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func keyColumnIndexes(columns []columnInfo, keyColumns []string) ([]int, error) {
	indexes := make([]int, 0, len(keyColumns))
	for _, keyColumn := range keyColumns {
		index := -1
		for i, column := range columns {
			if column.Name == keyColumn {
				index = i
				break
			}
		}
		if index < 0 {
			return nil, fmt.Errorf("key column %q is not present in selected column list", keyColumn)
		}
		indexes = append(indexes, index)
	}
	return indexes, nil
}

func keyCursorFromRow(rowValues []any, keyIndexes []int) []any {
	cursor := make([]any, 0, len(keyIndexes))
	for _, keyIndex := range keyIndexes {
		cursor = append(cursor, rowValues[keyIndex])
	}
	return cursor
}

func incompatibleStableKeyError(databaseName string, tableName string) error {
	return fmt.Errorf(
		"incompatible_for_v1_deterministic_hash: %s.%s has no primary key or non-null unique key; add a stable key before using hash/full-hash verify modes",
		databaseName,
		tableName,
	)
}

func buildSelectSQL(databaseName string, tableName string, columns []columnInfo) string {
	quotedColumns := make([]string, 0, len(columns))
	for _, column := range columns {
		quotedColumns = append(quotedColumns, quoteIdentifier(column.Name))
	}
	columnList := strings.Join(quotedColumns, ", ")
	return fmt.Sprintf(
		"SELECT %s FROM %s.%s",
		columnList,
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
	)
}

func buildOrderedSelectSQL(databaseName string, tableName string, columns []columnInfo, keyColumns []string, withLimit bool) string {
	base := buildSelectSQL(databaseName, tableName, columns)
	orderColumns := make([]string, 0, len(keyColumns))
	for _, column := range keyColumns {
		orderColumns = append(orderColumns, quoteIdentifier(column))
	}
	query := base + " ORDER BY " + strings.Join(orderColumns, ", ")
	if withLimit {
		query += " LIMIT ?"
	}
	return query
}

func buildKeysetSelectSQL(databaseName string, tableName string, columns []columnInfo, keyColumns []string, withCursor bool) string {
	base := buildSelectSQL(databaseName, tableName, columns)
	orderColumns := make([]string, 0, len(keyColumns))
	for _, column := range keyColumns {
		orderColumns = append(orderColumns, quoteIdentifier(column))
	}
	if withCursor {
		return fmt.Sprintf(
			"%s WHERE (%s) > (%s) ORDER BY %s LIMIT ?",
			base,
			strings.Join(orderColumns, ", "),
			strings.Join(repeat("?", len(keyColumns)), ", "),
			strings.Join(orderColumns, ", "),
		)
	}
	return fmt.Sprintf("%s ORDER BY %s LIMIT ?", base, strings.Join(orderColumns, ", "))
}

func listStableKeyColumns(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) ([]string, error) {
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
	defer func() {
		_ = rows.Close()
	}()

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

	notNullColumns, err := listNotNullColumns(ctx, queryer, databaseName, tableName)
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

func listNotNullColumns(ctx context.Context, queryer sqlQueryer, databaseName string, tableName string) (map[string]struct{}, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND IS_NULLABLE = 'NO'
	`, databaseName, tableName)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

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

func repeat(value string, count int) []string {
	if count <= 0 {
		return nil
	}
	out := make([]string, count)
	for i := 0; i < count; i++ {
		out[i] = value
	}
	return out
}

func normalizeHashValue(value any, info columnInfo) string {
	switch typed := value.(type) {
	case nil:
		return "null:"
	case []byte:
		if shouldTreatAsText(info) {
			return normalizeTextHashValue(string(typed), info)
		}
		return "bytes:" + base64.StdEncoding.EncodeToString(typed)
	case string:
		return normalizeTextHashValue(typed, info)
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

func normalizeTextHashValue(value string, info columnInfo) string {
	trimmed := strings.TrimSpace(value)
	if maybeJSONValue(info, trimmed) {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			canonical, err := json.Marshal(decoded)
			if err == nil {
				return "json:" + string(canonical)
			}
		}
	}
	return "string:" + value
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

func diffTableHashes(databaseName string, source map[string]string, dest map[string]string, risks map[string]TableRisk) ([]Diff, int, int, int, int, int) {
	diffs := []Diff{}
	compared := 0
	missingDestination := 0
	missingSource := 0
	hashMismatch := 0
	noiseRiskMismatch := 0

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
			noiseRisk := ""
			var notes []string
			if risk, ok := risks[tableName]; ok && len(risk.Notes) > 0 {
				noiseRisk = "representation_sensitive"
				notes = append(notes, risk.Notes...)
				noiseRiskMismatch++
			}
			diffs = append(diffs, Diff{
				Kind:       diffKindTableHashMismatch,
				Database:   databaseName,
				Table:      tableName,
				SourceHash: sourceHash,
				DestHash:   destHash,
				NoiseRisk:  noiseRisk,
				Notes:      notes,
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

	return diffs, compared, missingDestination, missingSource, hashMismatch, noiseRiskMismatch
}

func prepareVerifySession(ctx context.Context, queryer sqlQueryer) error {
	if _, err := queryer.ExecContext(ctx, "SET NAMES utf8mb4"); err != nil {
		return err
	}
	_, err := queryer.ExecContext(ctx, "SET SESSION time_zone = '+00:00'")
	return err
}

func tableRiskProfiles(ctx context.Context, queryer sqlQueryer, databases []string) (map[string]map[string]TableRisk, error) {
	if len(databases) == 0 {
		return map[string]map[string]TableRisk{}, nil
	}

	placeholders, args := placeholdersForStrings(databases)
	rows, err := queryer.QueryContext(ctx, fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, DATA_TYPE, COALESCE(COLLATION_NAME, '')
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA IN (%s)
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION
	`, placeholders), args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	out := map[string]map[string]TableRisk{}
	for rows.Next() {
		var databaseName string
		var tableName string
		var dataType string
		var collationName string
		if err := rows.Scan(&databaseName, &tableName, &dataType, &collationName); err != nil {
			return nil, err
		}
		if _, ok := out[databaseName]; !ok {
			out[databaseName] = map[string]TableRisk{}
		}
		risk := out[databaseName][tableName]
		risk.Database = databaseName
		risk.Table = tableName
		switch strings.ToLower(strings.TrimSpace(dataType)) {
		case "float", "double", "real":
			risk.ApproximateNumericColumns++
		case "timestamp", "datetime", "date", "time":
			risk.TemporalColumns++
		case "json":
			risk.JSONColumns++
		}
		if strings.TrimSpace(collationName) != "" {
			risk.CollationSensitiveColumns++
		}
		out[databaseName][tableName] = risk
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for databaseName, tables := range out {
		for tableName, risk := range tables {
			risk.Notes = buildRiskNotes(risk)
			tables[tableName] = risk
		}
		out[databaseName] = tables
	}
	return out, nil
}

func buildRiskNotes(risk TableRisk) []string {
	notes := make([]string, 0, 4)
	if risk.ApproximateNumericColumns > 0 {
		notes = append(notes, "Approximate numeric columns can produce representation-sensitive hash noise; confirm with row samples if hashes drift.")
	}
	if risk.TemporalColumns > 0 {
		notes = append(notes, "Temporal columns depend on session time_zone rendering; verify runs normalize sessions to UTC before hashing.")
	}
	if risk.JSONColumns > 0 {
		notes = append(notes, "JSON values are canonicalized before hashing, but semantic edge cases still deserve row-sample review.")
	}
	if risk.CollationSensitiveColumns > 0 {
		notes = append(notes, "Text ordering and collation differences can create false positives if hashing depends on SQL sort order; row hashes are sorted client-side here.")
	}
	return notes
}

func flattenTableRisks(risks map[string]map[string]TableRisk) []TableRisk {
	out := make([]TableRisk, 0, 8)
	for _, tables := range risks {
		for _, risk := range tables {
			if len(risk.Notes) == 0 {
				continue
			}
			out = append(out, risk)
		}
	}
	sort.Slice(out, func(i int, j int) bool {
		if out[i].Database != out[j].Database {
			return out[i].Database < out[j].Database
		}
		return out[i].Table < out[j].Table
	})
	return out
}

func maybeJSONValue(info columnInfo, value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if info.DataType == "json" {
		return true
	}
	if !shouldTreatAsText(info) {
		return false
	}
	first := trimmed[0]
	if first != '{' && first != '[' {
		return false
	}
	return json.Valid([]byte(trimmed))
}

func shouldTreatAsText(info columnInfo) bool {
	switch info.DataType {
	case "char", "varchar", "tinytext", "text", "mediumtext", "longtext", "enum", "set", "json":
		return true
	default:
		return false
	}
}

func placeholdersForStrings(values []string) (string, []any) {
	items := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, "?")
		args = append(args, value)
	}
	return strings.Join(items, ", "), args
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
