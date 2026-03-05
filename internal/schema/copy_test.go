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

func TestRewriteSchemaStatementForSandbox(t *testing.T) {
	in := "CREATE VIEW `srcdb`.`v1` AS SELECT * FROM `srcdb`.`t1`"
	out := rewriteSchemaStatementForSandbox(in, "srcdb", "dryrun_srcdb")
	if out != "CREATE VIEW `dryrun_srcdb`.`v1` AS SELECT * FROM `dryrun_srcdb`.`t1`" {
		t.Fatalf("unexpected rewritten statement: %q", out)
	}
}

func TestSortTableNamesByDependencies(t *testing.T) {
	tableNames := []string{"cart_items", "users", "orders"}
	dependencies := map[string]map[string]struct{}{
		"cart_items": {
			"users":  {},
			"orders": {},
		},
		"users":  {},
		"orders": {"users": {}},
	}

	got := sortTableNamesByDependencies(tableNames, dependencies)
	want := []string{"users", "orders", "cart_items"}

	if len(got) != len(want) {
		t.Fatalf("unexpected sorted length: got=%d want=%d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sorted order: got=%#v want=%#v", got, want)
		}
	}
}

func TestSortTableNamesByDependenciesCycleFallback(t *testing.T) {
	tableNames := []string{"b", "c", "a"}
	dependencies := map[string]map[string]struct{}{
		"a": {"b": {}},
		"b": {"a": {}},
		"c": {},
	}

	got := sortTableNamesByDependencies(tableNames, dependencies)
	want := []string{"c", "a", "b"}

	if len(got) != len(want) {
		t.Fatalf("unexpected sorted length: got=%d want=%d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected sorted order: got=%#v want=%#v", got, want)
		}
	}
}
