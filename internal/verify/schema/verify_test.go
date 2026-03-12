package schema

import (
	"context"
	"testing"
)

func TestNormalizeCreateStatementIgnoresVolatileFormatting(t *testing.T) {
	left := "CREATE TABLE `users` (\n  `id` bigint NOT NULL AUTO_INCREMENT,\n  `name` varchar(50) NOT NULL\n) ENGINE=InnoDB AUTO_INCREMENT=101"
	right := "create table users ( id bigint not null auto_increment, name varchar(50) not null ) engine=innodb auto_increment = 7;"

	leftNormalized := normalizeCreateStatement(left)
	rightNormalized := normalizeCreateStatement(right)
	if leftNormalized != rightNormalized {
		t.Fatalf("expected normalized statements to match\nleft=%q\nright=%q", leftNormalized, rightNormalized)
	}
}

func TestNormalizeCreateStatementIgnoresDefinerDifferences(t *testing.T) {
	left := "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER VIEW `v_users` AS select `users`.`id` AS `id` from `users`"
	right := "CREATE ALGORITHM=UNDEFINED DEFINER=`app`@`%` SQL SECURITY DEFINER VIEW `v_users` AS SELECT `users`.`id` AS `id` FROM `users`"

	leftNormalized := normalizeCreateStatement(left)
	rightNormalized := normalizeCreateStatement(right)
	if leftNormalized != rightNormalized {
		t.Fatalf("expected view definers to normalize equally\nleft=%q\nright=%q", leftNormalized, rightNormalized)
	}
}

func TestNormalizeCreateStatementPreservesQuotedCase(t *testing.T) {
	left := "CREATE VIEW `v_users` AS SELECT 'KeepMe' AS `Label`"
	right := "CREATE VIEW `v_users` AS SELECT 'keepme' AS `Label`"

	leftNormalized := normalizeCreateStatement(left)
	rightNormalized := normalizeCreateStatement(right)
	if leftNormalized == rightNormalized {
		t.Fatalf("expected quoted literal case drift to remain visible\nleft=%q\nright=%q", leftNormalized, rightNormalized)
	}
}

func TestDiffObjectMaps(t *testing.T) {
	source := map[string]objectDef{
		"table:users": {
			ObjectType: objectTypeTable,
			ObjectName: "users",
			CreateSQL:  "CREATE TABLE users (id INT PRIMARY KEY)",
			normalized: normalizeCreateStatement("CREATE TABLE users (id INT PRIMARY KEY)"),
		},
		"table:posts": {
			ObjectType: objectTypeTable,
			ObjectName: "posts",
			CreateSQL:  "CREATE TABLE posts (id INT PRIMARY KEY)",
			normalized: normalizeCreateStatement("CREATE TABLE posts (id INT PRIMARY KEY)"),
		},
	}
	dest := map[string]objectDef{
		"table:users": {
			ObjectType: objectTypeTable,
			ObjectName: "users",
			CreateSQL:  "CREATE TABLE users (id BIGINT PRIMARY KEY)",
			normalized: normalizeCreateStatement("CREATE TABLE users (id BIGINT PRIMARY KEY)"),
		},
		"view:v_users": {
			ObjectType: objectTypeView,
			ObjectName: "v_users",
			CreateSQL:  "CREATE VIEW v_users AS SELECT id FROM users",
			normalized: normalizeCreateStatement("CREATE VIEW v_users AS SELECT id FROM users"),
		},
	}

	diffs, compared, missingDest, missingSource, mismatches := diffObjectMaps("app", source, dest)
	if compared != 1 {
		t.Fatalf("expected compared=1, got %d", compared)
	}
	if missingDest != 1 {
		t.Fatalf("expected missing destination=1, got %d", missingDest)
	}
	if missingSource != 1 {
		t.Fatalf("expected missing source=1, got %d", missingSource)
	}
	if mismatches != 1 {
		t.Fatalf("expected mismatches=1, got %d", mismatches)
	}
	if len(diffs) != 3 {
		t.Fatalf("expected 3 diffs, got %d", len(diffs))
	}
}

func TestVerifyRequiresAtLeastOneObjectType(t *testing.T) {
	_, err := Verify(context.Background(), nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error when no object type enabled")
	}
}

func TestObjectTypeConstants(t *testing.T) {
	if objectTypeProcedure != "procedure" {
		t.Fatalf("unexpected procedure constant: %q", objectTypeProcedure)
	}
	if objectTypeFunction != "function" {
		t.Fatalf("unexpected function constant: %q", objectTypeFunction)
	}
	if objectTypeTrigger != "trigger" {
		t.Fatalf("unexpected trigger constant: %q", objectTypeTrigger)
	}
	if objectTypeEvent != "event" {
		t.Fatalf("unexpected event constant: %q", objectTypeEvent)
	}
}

func TestUnionAndSort(t *testing.T) {
	values := unionAndSort([]string{"b", "a", " "}, []string{"a", "c"})
	expected := []string{"a", "b", "c"}
	if len(values) != len(expected) {
		t.Fatalf("unexpected union length: got=%d want=%d", len(values), len(expected))
	}
	for i := range expected {
		if values[i] != expected[i] {
			t.Fatalf("unexpected union item at %d: got=%q want=%q", i, values[i], expected[i])
		}
	}
}
