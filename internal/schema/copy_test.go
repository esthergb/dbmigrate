package schema

import "testing"

func TestSelectDatabasesIncludeExclude(t *testing.T) {
	all := []string{"information_schema", "db1", "db2", "db3", "mysql"}
	include := []string{"db1", "db3", "missing"}
	exclude := []string{"db3"}

	selected := SelectDatabases(all, include, exclude)
	if len(selected) != 1 || selected[0] != "db1" {
		t.Fatalf("unexpected selected databases: %#v", selected)
	}
}

func TestSelectDatabasesDefaultExcludesSystem(t *testing.T) {
	all := []string{"information_schema", "db1", "sys", "mysql", "db2"}
	selected := SelectDatabases(all, nil, nil)

	if len(selected) != 2 {
		t.Fatalf("expected 2 user databases, got %d (%#v)", len(selected), selected)
	}
	if selected[0] != "db1" || selected[1] != "db2" {
		t.Fatalf("unexpected selected databases order/content: %#v", selected)
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	in := "we`rd"
	out := quoteIdentifier(in)
	if out != "`we``rd`" {
		t.Fatalf("unexpected quoted identifier: %s", out)
	}
}
