package schema

import (
	"context"
	"testing"
)

func TestExtractResultEmpty(t *testing.T) {
	r := extractResult{}
	if !r.empty() {
		t.Fatal("expected empty result to report empty")
	}
	r.statements = []string{"CREATE TABLE x (id INT)"}
	if r.empty() {
		t.Fatal("expected non-empty result to report not empty")
	}
}

func TestFetchRoutineCreateStatementUnknownType(t *testing.T) {
	_, err := fetchRoutineCreateStatement(context.Background(), nil, "db", "fn", "AGGREGATE")
	if err == nil {
		t.Fatal("expected error for unknown routine type")
	}
}

func TestCopySchemaRequiresAtLeastOneObjectType(t *testing.T) {
	_, err := CopySchema(context.Background(), nil, nil, CopyOptions{})
	if err == nil {
		t.Fatal("expected error when no object type enabled")
	}
}

func TestValidateSandboxRequiresAtLeastOneObjectType(t *testing.T) {
	_, err := ValidateSchemaInSandbox(context.Background(), nil, nil, DryRunSandboxOptions{
		MapDatabase: func(s string) string { return s },
	})
	if err == nil {
		t.Fatal("expected error when no object type enabled")
	}
}

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

func TestSanitizeCreateStatementForApplyRewritesDefiner(t *testing.T) {
	in := "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER VIEW `v_users` AS SELECT 1 AS `id`"
	out := sanitizeCreateStatementForApply(in)
	if out == in {
		t.Fatal("expected statement to change")
	}
	if out != "CREATE ALGORITHM=UNDEFINED DEFINER=CURRENT_USER SQL SECURITY DEFINER VIEW `v_users` AS SELECT 1 AS `id`" {
		t.Fatalf("unexpected sanitized statement: %q", out)
	}
}

func TestRewriteAndSanitizeForSandbox(t *testing.T) {
	in := "CREATE ALGORITHM=UNDEFINED DEFINER=`app`@`%` SQL SECURITY DEFINER VIEW `srcdb`.`v1` AS SELECT * FROM `srcdb`.`t1`"
	rewritten := rewriteSchemaStatementForSandbox(in, "srcdb", "dryrun_srcdb")
	out := sanitizeCreateStatementForApply(rewritten)
	if out != "CREATE ALGORITHM=UNDEFINED DEFINER=CURRENT_USER SQL SECURITY DEFINER VIEW `dryrun_srcdb`.`v1` AS SELECT * FROM `dryrun_srcdb`.`t1`" {
		t.Fatalf("unexpected rewritten+sanitized statement: %q", out)
	}
}

func TestRewriteSchemaStatementForSandboxSkipsCommentsAndLiterals(t *testing.T) {
	in := "CREATE VIEW `srcdb`.`v1` AS SELECT 'from `srcdb`.literal' AS note /* `srcdb`.`ignored` */ FROM `srcdb`.`t1` -- `srcdb`.`comment`\n"
	out := rewriteSchemaStatementForSandbox(in, "srcdb", "dryrun_srcdb")
	want := "CREATE VIEW `dryrun_srcdb`.`v1` AS SELECT 'from `srcdb`.literal' AS note /* `srcdb`.`ignored` */ FROM `dryrun_srcdb`.`t1` -- `srcdb`.`comment`\n"
	if out != want {
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

	ordered, cyclic := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	if len(ordered) != len(want) {
		t.Fatalf("unexpected detailed sorted length: got=%d want=%d (%#v)", len(ordered), len(want), ordered)
	}
	if len(cyclic) != 0 {
		t.Fatalf("unexpected cyclic tables: %#v", cyclic)
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

	ordered, cyclic := sortTableNamesByDependenciesDetailed(tableNames, dependencies)
	if len(ordered) != len(want) {
		t.Fatalf("unexpected detailed sorted length: got=%d want=%d (%#v)", len(ordered), len(want), ordered)
	}
	if len(cyclic) != 2 || cyclic[0] != "a" || cyclic[1] != "b" {
		t.Fatalf("unexpected cyclic tables: %#v", cyclic)
	}
}
