package cdc

import (
	"context"
	"strings"
	"testing"
)

func TestTriggerNameShort(t *testing.T) {
	name := triggerName("ins", "orders")
	if !strings.HasPrefix(name, triggerPrefix) {
		t.Fatalf("expected prefix %q in %q", triggerPrefix, name)
	}
	if len(name) > triggerMaxNameLen {
		t.Fatalf("trigger name too long: %q (%d)", name, len(name))
	}
}

func TestTriggerNameLong(t *testing.T) {
	longTable := strings.Repeat("a", 60)
	name := triggerName("ins", longTable)
	if len(name) > triggerMaxNameLen {
		t.Fatalf("trigger name too long: %q (%d)", name, len(name))
	}
}

func TestTriggerNameDeterministic(t *testing.T) {
	a := triggerName("upd", "my_table")
	b := triggerName("upd", "my_table")
	if a != b {
		t.Fatalf("trigger names not deterministic: %q vs %q", a, b)
	}
}

func TestTriggerNameDistinct(t *testing.T) {
	ins := triggerName("ins", "orders")
	upd := triggerName("upd", "orders")
	del := triggerName("del", "orders")
	if ins == upd || ins == del || upd == del {
		t.Fatalf("trigger names not distinct: %q %q %q", ins, upd, del)
	}
}

func TestBuildJSONObjectEmpty(t *testing.T) {
	result := buildJSONObject("NEW", nil)
	if result != "JSON_OBJECT()" {
		t.Fatalf("unexpected result for empty columns: %q", result)
	}
}

func TestBuildJSONObjectColumns(t *testing.T) {
	result := buildJSONObject("NEW", []string{"id", "name"})
	if !strings.Contains(result, "'id'") || !strings.Contains(result, "'name'") {
		t.Fatalf("expected column names in JSON_OBJECT: %q", result)
	}
	if !strings.Contains(result, "NEW.`id`") || !strings.Contains(result, "NEW.`name`") {
		t.Fatalf("expected NEW.col references in JSON_OBJECT: %q", result)
	}
}

func TestGenerateInsertTrigger(t *testing.T) {
	ddl := generateInsertTrigger("testdb", "orders", []string{"id", "amount"})
	if !strings.Contains(ddl, "AFTER INSERT") {
		t.Fatalf("expected AFTER INSERT in trigger DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "INSERT INTO") {
		t.Fatalf("expected INSERT INTO CDC log in trigger DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "'INSERT'") {
		t.Fatalf("expected 'INSERT' operation in trigger DDL: %q", ddl)
	}
}

func TestGenerateUpdateTrigger(t *testing.T) {
	ddl := generateUpdateTrigger("testdb", "orders", []string{"id", "amount"})
	if !strings.Contains(ddl, "AFTER UPDATE") {
		t.Fatalf("expected AFTER UPDATE in trigger DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "OLD.") || !strings.Contains(ddl, "NEW.") {
		t.Fatalf("expected OLD and NEW row refs in update trigger: %q", ddl)
	}
}

func TestGenerateDeleteTrigger(t *testing.T) {
	ddl := generateDeleteTrigger("testdb", "orders", []string{"id", "amount"})
	if !strings.Contains(ddl, "AFTER DELETE") {
		t.Fatalf("expected AFTER DELETE in trigger DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "'DELETE'") {
		t.Fatalf("expected 'DELETE' operation in trigger DDL: %q", ddl)
	}
}

func TestCDCLogTableDDL(t *testing.T) {
	ddl := cdcLogTableDDL("mydb")
	if !strings.Contains(ddl, "__dbmigrate_cdc_log") {
		t.Fatalf("expected log table name in DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "cdc_id") {
		t.Fatalf("expected cdc_id column in DDL: %q", ddl)
	}
	if !strings.Contains(ddl, "AUTO_INCREMENT") {
		t.Fatalf("expected AUTO_INCREMENT in DDL: %q", ddl)
	}
}

func TestSetupCDCNilConnection(t *testing.T) {
	err := SetupCDC(context.TODO(), nil, "mydb", []string{"orders"})
	if err == nil {
		t.Fatal("expected error for nil source connection")
	}
}

func TestTeardownCDCNilConnection(t *testing.T) {
	err := TeardownCDC(context.TODO(), nil, "mydb", []string{"orders"})
	if err == nil {
		t.Fatal("expected error for nil source connection")
	}
}

func TestQuoteIdentifier(t *testing.T) {
	got := quoteIdentifier("my`col")
	want := "`my``col`"
	if got != want {
		t.Fatalf("quoteIdentifier(%q) = %q, want %q", "my`col", got, want)
	}
}

func TestQuoteLiteral(t *testing.T) {
	got := quoteLiteral("it's")
	if !strings.Contains(got, `\'`) {
		t.Fatalf("quoteLiteral did not escape single quote: %q", got)
	}
}
