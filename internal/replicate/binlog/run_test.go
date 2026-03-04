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

func TestParseLogBinEnabled(t *testing.T) {
	cases := []struct {
		in   any
		want bool
	}{
		{in: int64(1), want: true},
		{in: int64(0), want: false},
		{in: "ON", want: true},
		{in: "off", want: false},
		{in: []byte("TRUE"), want: true},
		{in: []byte("FALSE"), want: false},
	}

	for _, tc := range cases {
		got, err := parseLogBinEnabled(tc.in)
		if err != nil {
			t.Fatalf("parseLogBinEnabled(%v): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("parseLogBinEnabled(%v)=%v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseLogBinEnabledInvalid(t *testing.T) {
	if _, err := parseLogBinEnabled("MAYBE"); err == nil {
		t.Fatal("expected invalid log_bin value to fail")
	}
}

func TestNormalizeBinlogFormat(t *testing.T) {
	if got := normalizeBinlogFormat("row"); got != "ROW" {
		t.Fatalf("unexpected normalized format: %q", got)
	}
	if got := normalizeBinlogFormat([]byte("mixed")); got != "MIXED" {
		t.Fatalf("unexpected normalized format: %q", got)
	}
}
