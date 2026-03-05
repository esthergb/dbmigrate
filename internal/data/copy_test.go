package data

import "testing"

func TestBuildInsertSQL(t *testing.T) {
	sql := buildInsertSQL("app", "users", []string{"id", "name"})
	expected := "INSERT INTO `app`.`users` (`id`, `name`) VALUES (?, ?)"
	if sql != expected {
		t.Fatalf("unexpected insert SQL: %s", sql)
	}
}

func TestBuildSelectSQL(t *testing.T) {
	sql := buildSelectSQL("app", "users", []string{"id", "name"})
	expected := "SELECT `id`, `name` FROM `app`.`users` LIMIT ? OFFSET ?"
	if sql != expected {
		t.Fatalf("unexpected select SQL: %s", sql)
	}
}

func TestQuoteIdentifierEscapesBackticks(t *testing.T) {
	out := quoteIdentifier("na`me")
	if out != "`na``me`" {
		t.Fatalf("unexpected quoted identifier: %s", out)
	}
}

func TestCountPlaceholders(t *testing.T) {
	insertSQL := "INSERT INTO `app`.`users` (`id`, `name`, `email`) VALUES (?, ?, ?)"
	if got := countPlaceholders(insertSQL); got != 3 {
		t.Fatalf("expected 3 placeholders, got %d", got)
	}
}
