package binlog

import "testing"

func TestValidateApplyDDL(t *testing.T) {
	valid := []string{"ignore", "apply", "warn"}
	for _, value := range valid {
		if err := validateApplyDDL(value); err != nil {
			t.Fatalf("expected %q to be valid: %v", value, err)
		}
	}

	if err := validateApplyDDL("invalid"); err == nil {
		t.Fatal("expected invalid apply-ddl value to fail")
	}
}

func TestExtractBinlogPositionMySQLColumns(t *testing.T) {
	columns := []string{"File", "Position", "Binlog_Do_DB"}
	values := []any{[]byte("mysql-bin.000123"), int64(456), nil}

	file, pos, err := extractBinlogPosition(columns, values)
	if err != nil {
		t.Fatalf("extract binlog position: %v", err)
	}
	if file != "mysql-bin.000123" {
		t.Fatalf("unexpected file: %q", file)
	}
	if pos != 456 {
		t.Fatalf("unexpected pos: %d", pos)
	}
}

func TestExtractBinlogPositionMariaColumns(t *testing.T) {
	columns := []string{"Log_name", "Pos"}
	values := []any{[]byte("mariadb-bin.000007"), []byte("8910")}

	file, pos, err := extractBinlogPosition(columns, values)
	if err != nil {
		t.Fatalf("extract binlog position: %v", err)
	}
	if file != "mariadb-bin.000007" {
		t.Fatalf("unexpected file: %q", file)
	}
	if pos != 8910 {
		t.Fatalf("unexpected pos: %d", pos)
	}
}

func TestExtractBinlogPositionMissingColumns(t *testing.T) {
	columns := []string{"a", "b"}
	values := []any{"x", "y"}
	if _, _, err := extractBinlogPosition(columns, values); err == nil {
		t.Fatal("expected missing columns to fail")
	}
}
