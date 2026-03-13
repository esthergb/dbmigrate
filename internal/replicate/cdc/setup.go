package cdc

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	cdcLogTable       = "__dbmigrate_cdc_log"
	triggerMaxNameLen = 64
	triggerPrefix     = "__dbmigrate_"
)

func cdcLogTableName(schema string) string {
	return quoteIdentifier(schema) + "." + quoteIdentifier(cdcLogTable)
}

func triggerName(prefix string, table string) string {
	name := triggerPrefix + prefix + "_" + table
	if len(name) <= triggerMaxNameLen {
		return name
	}
	const hashLen = 8
	overhead := len(triggerPrefix) + len(prefix) + 1 + hashLen
	maxTable := triggerMaxNameLen - overhead
	if maxTable < 1 {
		maxTable = 1
	}
	hash := fnv32(table)
	return fmt.Sprintf("%s%s_%.*s%08x", triggerPrefix, prefix, maxTable, table, hash)
}

func fnv32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

func quoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

func cdcLogTableDDL(schema string) string {
	return fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  cdc_id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  table_name VARCHAR(255) NOT NULL,
  operation ENUM('INSERT','UPDATE','DELETE') NOT NULL,
  old_row_json LONGTEXT,
  new_row_json LONGTEXT,
  captured_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
) ENGINE=InnoDB`, cdcLogTableName(schema))
}

func generateInsertTrigger(schema string, table string, columns []string) string {
	name := triggerName("ins", table)
	jsonExpr := buildJSONObject("NEW", columns)
	return fmt.Sprintf(
		"CREATE TRIGGER %s AFTER INSERT ON %s FOR EACH ROW INSERT INTO %s (table_name, operation, new_row_json) VALUES (%s, 'INSERT', %s)",
		quoteIdentifier(name),
		quoteIdentifier(schema)+"."+quoteIdentifier(table),
		cdcLogTableName(schema),
		quoteLiteral(table),
		jsonExpr,
	)
}

func generateUpdateTrigger(schema string, table string, columns []string) string {
	name := triggerName("upd", table)
	oldJSON := buildJSONObject("OLD", columns)
	newJSON := buildJSONObject("NEW", columns)
	return fmt.Sprintf(
		"CREATE TRIGGER %s AFTER UPDATE ON %s FOR EACH ROW INSERT INTO %s (table_name, operation, old_row_json, new_row_json) VALUES (%s, 'UPDATE', %s, %s)",
		quoteIdentifier(name),
		quoteIdentifier(schema)+"."+quoteIdentifier(table),
		cdcLogTableName(schema),
		quoteLiteral(table),
		oldJSON,
		newJSON,
	)
}

func generateDeleteTrigger(schema string, table string, columns []string) string {
	name := triggerName("del", table)
	jsonExpr := buildJSONObject("OLD", columns)
	return fmt.Sprintf(
		"CREATE TRIGGER %s AFTER DELETE ON %s FOR EACH ROW INSERT INTO %s (table_name, operation, old_row_json) VALUES (%s, 'DELETE', %s)",
		quoteIdentifier(name),
		quoteIdentifier(schema)+"."+quoteIdentifier(table),
		cdcLogTableName(schema),
		quoteLiteral(table),
		jsonExpr,
	)
}

func buildJSONObject(rowRef string, columns []string) string {
	if len(columns) == 0 {
		return "JSON_OBJECT()"
	}
	parts := make([]string, 0, len(columns)*2)
	for _, col := range columns {
		parts = append(parts, quoteLiteral(col), rowRef+"."+quoteIdentifier(col))
	}
	return "JSON_OBJECT(" + strings.Join(parts, ", ") + ")"
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`) + "'"
}

var listTableColumnsFn = listTableColumns

func listTableColumns(ctx context.Context, db *sql.DB, schema string, table string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT COLUMN_NAME FROM INFORMATION_SCHEMA.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`,
		schema, table,
	)
	if err != nil {
		return nil, fmt.Errorf("list columns for %s.%s: %w", schema, table, err)
	}
	defer func() { _ = rows.Close() }()

	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("scan column for %s.%s: %w", schema, table, err)
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

// SetupCDC creates the CDC log table and triggers for the given tables in a schema.
func SetupCDC(ctx context.Context, source *sql.DB, schema string, tables []string) error {
	if source == nil {
		return fmt.Errorf("setup CDC: source connection is required")
	}
	if _, err := source.ExecContext(ctx, cdcLogTableDDL(schema)); err != nil {
		return fmt.Errorf("create CDC log table for %s: %w", schema, err)
	}
	for _, table := range tables {
		cols, err := listTableColumnsFn(ctx, source, schema, table)
		if err != nil {
			return fmt.Errorf("setup CDC triggers for %s.%s: %w", schema, table, err)
		}
		for _, ddl := range []string{
			generateInsertTrigger(schema, table, cols),
			generateUpdateTrigger(schema, table, cols),
			generateDeleteTrigger(schema, table, cols),
		} {
			if _, err := source.ExecContext(ctx, ddl); err != nil {
				return fmt.Errorf("create CDC trigger for %s.%s: %w", schema, table, err)
			}
		}
	}
	return nil
}

// TeardownCDC drops CDC triggers and the log table for the given tables in a schema.
func TeardownCDC(ctx context.Context, source *sql.DB, schema string, tables []string) error {
	if source == nil {
		return fmt.Errorf("teardown CDC: source connection is required")
	}
	for _, table := range tables {
		for _, prefix := range []string{"ins", "upd", "del"} {
			name := triggerName(prefix, table)
			ddl := fmt.Sprintf("DROP TRIGGER IF EXISTS %s.%s", quoteIdentifier(schema), quoteIdentifier(name))
			if _, err := source.ExecContext(ctx, ddl); err != nil {
				return fmt.Errorf("drop CDC trigger %s for %s.%s: %w", name, schema, table, err)
			}
		}
	}
	ddl := fmt.Sprintf("DROP TABLE IF EXISTS %s", cdcLogTableName(schema))
	if _, err := source.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("drop CDC log table for %s: %w", schema, err)
	}
	return nil
}
