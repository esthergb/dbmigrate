package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/esthergb/dbmigrate/internal/schema"
	"github.com/esthergb/dbmigrate/internal/state"
)

// CopyOptions controls baseline data-copy behavior.
type CopyOptions struct {
	IncludeDatabases []string
	ExcludeDatabases []string
	ChunkSize        int
	Resume           bool
	RequireEmptyDest bool
}

// CopySummary reports copied data metrics.
type CopySummary struct {
	Databases      int
	Tables         int
	Completed      int
	Restarted      int
	RowsCopied     int64
	Batches        int
	CheckpointFile string
}

// CopyBaselineData copies table rows in batches with checkpoint/resume support.
func CopyBaselineData(ctx context.Context, source *sql.DB, dest *sql.DB, stateDir string, opts CopyOptions) (CopySummary, error) {
	if source == nil || dest == nil {
		return CopySummary{}, errors.New("source and destination connections are required")
	}
	if opts.ChunkSize < 1 {
		return CopySummary{}, errors.New("chunk size must be >= 1")
	}

	databases, err := listDatabases(ctx, source)
	if err != nil {
		return CopySummary{}, fmt.Errorf("list source databases: %w", err)
	}
	selected := schema.SelectDatabases(databases, opts.IncludeDatabases, opts.ExcludeDatabases)

	checkpointFile := filepath.Join(stateDir, "data-baseline-checkpoint.json")
	checkpoint := state.NewDataCheckpoint()
	if opts.Resume {
		checkpoint, err = state.LoadDataCheckpoint(checkpointFile)
		if err != nil {
			return CopySummary{}, err
		}
	}

	summary := CopySummary{CheckpointFile: checkpointFile}
	for _, databaseName := range selected {
		tableNames, err := listBaseTables(ctx, source, databaseName)
		if err != nil {
			return summary, fmt.Errorf("list source tables for %s: %w", databaseName, err)
		}
		if len(tableNames) == 0 {
			continue
		}
		summary.Databases++
		summary.Tables += len(tableNames)

		if opts.RequireEmptyDest {
			if err := ensureDestinationTablesAreEmpty(ctx, dest, databaseName, tableNames); err != nil {
				return summary, fmt.Errorf("destination non-empty check failed for %s: %w", databaseName, err)
			}
		}

		for _, tableName := range tableNames {
			tableKey := databaseName + "." + tableName
			progress := checkpoint.Tables[tableKey]
			if progress.Done {
				summary.Completed++
				continue
			}

			if opts.Resume && progress.RowsCopied > 0 {
				if err := resetDestinationTable(ctx, dest, databaseName, tableName); err != nil {
					return summary, fmt.Errorf("reset destination table %s: %w", tableKey, err)
				}
				progress.RowsCopied = 0
				progress.Done = false
				progress.UpdatedAt = time.Now().UTC()
				checkpoint.Tables[tableKey] = progress
				if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
					return summary, err
				}
				summary.Restarted++
			}

			columns, err := listTableColumns(ctx, source, databaseName, tableName)
			if err != nil {
				return summary, fmt.Errorf("list columns for %s: %w", tableKey, err)
			}
			if len(columns) == 0 {
				progress.Done = true
				progress.UpdatedAt = time.Now().UTC()
				checkpoint.Tables[tableKey] = progress
				if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
					return summary, err
				}
				summary.Completed++
				continue
			}

			selectSQL := buildSelectSQL(databaseName, tableName, columns)
			insertSQL := buildInsertSQL(databaseName, tableName, columns)
			offset := int64(0)
			for {
				batch, err := fetchBatch(ctx, source, selectSQL, opts.ChunkSize, offset, len(columns))
				if err != nil {
					return summary, fmt.Errorf("fetch batch for %s: %w", tableKey, err)
				}
				if len(batch) == 0 {
					break
				}

				if err := applyBatch(ctx, dest, insertSQL, batch); err != nil {
					return summary, fmt.Errorf("apply batch for %s: %w", tableKey, err)
				}

				offset += int64(len(batch))
				progress.RowsCopied = offset
				progress.UpdatedAt = time.Now().UTC()
				checkpoint.Tables[tableKey] = progress
				if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
					return summary, err
				}

				summary.RowsCopied += int64(len(batch))
				summary.Batches++

				if len(batch) < opts.ChunkSize {
					break
				}
			}

			progress.Done = true
			progress.UpdatedAt = time.Now().UTC()
			checkpoint.Tables[tableKey] = progress
			if err := state.SaveDataCheckpoint(checkpointFile, checkpoint); err != nil {
				return summary, err
			}
			summary.Completed++
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

	var out []string
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

func buildSelectSQL(databaseName string, tableName string, columns []string) string {
	parts := make([]string, 0, len(columns))
	for _, col := range columns {
		parts = append(parts, quoteIdentifier(col))
	}
	return fmt.Sprintf(
		"SELECT %s FROM %s.%s LIMIT ? OFFSET ?",
		strings.Join(parts, ", "),
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
	)
}

func buildInsertSQL(databaseName string, tableName string, columns []string) string {
	colParts := make([]string, 0, len(columns))
	placeholders := make([]string, 0, len(columns))
	for _, col := range columns {
		colParts = append(colParts, quoteIdentifier(col))
		placeholders = append(placeholders, "?")
	}
	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s) VALUES (%s)",
		quoteIdentifier(databaseName),
		quoteIdentifier(tableName),
		strings.Join(colParts, ", "),
		strings.Join(placeholders, ", "),
	)
}

func fetchBatch(ctx context.Context, db *sql.DB, selectSQL string, chunkSize int, offset int64, expectedColumns int) ([][]any, error) {
	rows, err := db.QueryContext(ctx, selectSQL, chunkSize, offset)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()

	batch := make([][]any, 0, chunkSize)
	for rows.Next() {
		rowValues := make([]any, expectedColumns)
		scanValues := make([]any, expectedColumns)
		for i := range rowValues {
			scanValues[i] = &rowValues[i]
		}
		if err := rows.Scan(scanValues...); err != nil {
			return nil, err
		}

		for i, val := range rowValues {
			if raw, ok := val.([]byte); ok {
				copied := make([]byte, len(raw))
				copy(copied, raw)
				rowValues[i] = copied
			}
		}

		batch = append(batch, rowValues)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return batch, nil
}

func applyBatch(ctx context.Context, db *sql.DB, insertSQL string, batch [][]any) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	for _, row := range batch {
		if _, err := tx.ExecContext(ctx, insertSQL, row...); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return err
	}
	return nil
}

func ensureDestinationTablesAreEmpty(ctx context.Context, db *sql.DB, databaseName string, tableNames []string) error {
	sorted := append([]string(nil), tableNames...)
	sort.Strings(sorted)

	for _, tableName := range sorted {
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
		var count int64
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("table %s.%s already contains %d rows", databaseName, tableName, count)
		}
	}
	return nil
}

func resetDestinationTable(ctx context.Context, db *sql.DB, databaseName string, tableName string) error {
	truncate := fmt.Sprintf("TRUNCATE TABLE %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
	if _, err := db.ExecContext(ctx, truncate); err == nil {
		return nil
	}

	deleteAll := fmt.Sprintf("DELETE FROM %s.%s", quoteIdentifier(databaseName), quoteIdentifier(tableName))
	if _, err := db.ExecContext(ctx, deleteAll); err != nil {
		return err
	}
	return nil
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}
