package cdc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// CDCEvent is one row read from the CDC log table.
type CDCEvent struct {
	CDCID      uint64
	TableName  string
	Operation  string
	OldRowJSON string
	NewRowJSON string
}

// ReadCDCEvents reads up to limit events from the CDC log table starting after fromID.
func ReadCDCEvents(ctx context.Context, source *sql.DB, schema string, fromID uint64, limit int) ([]CDCEvent, error) {
	if source == nil {
		return nil, fmt.Errorf("read CDC events: source connection is required")
	}
	query := fmt.Sprintf(
		`SELECT cdc_id, table_name, operation, COALESCE(old_row_json,''), COALESCE(new_row_json,'')
		 FROM %s WHERE cdc_id > ? ORDER BY cdc_id LIMIT ?`,
		cdcLogTableName(schema),
	)
	rows, err := source.QueryContext(ctx, query, fromID, limit)
	if err != nil {
		return nil, fmt.Errorf("read CDC log for %s: %w", schema, err)
	}
	defer func() { _ = rows.Close() }()

	var events []CDCEvent
	for rows.Next() {
		var e CDCEvent
		if err := rows.Scan(&e.CDCID, &e.TableName, &e.Operation, &e.OldRowJSON, &e.NewRowJSON); err != nil {
			return nil, fmt.Errorf("scan CDC event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate CDC events: %w", err)
	}
	return events, nil
}

// PurgeCDCEvents deletes CDC log entries with cdc_id <= upToID.
func PurgeCDCEvents(ctx context.Context, source *sql.DB, schema string, upToID uint64) error {
	if source == nil {
		return fmt.Errorf("purge CDC events: source connection is required")
	}
	_, err := source.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE cdc_id <= ?", cdcLogTableName(schema)),
		upToID,
	)
	if err != nil {
		return fmt.Errorf("purge CDC log for %s up to %d: %w", schema, upToID, err)
	}
	return nil
}

// ParseJSONRow parses a JSON object into a map of column→value.
func ParseJSONRow(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("parse CDC row JSON: %w", err)
	}
	return m, nil
}

// RowToValues converts a JSON row map to an ordered []any slice using the column order.
func RowToValues(row map[string]any, columns []string) []any {
	vals := make([]any, len(columns))
	for i, col := range columns {
		vals[i] = row[col]
	}
	return vals
}
