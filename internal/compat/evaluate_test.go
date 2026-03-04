package compat

import "testing"

func TestParseInstanceMySQL(t *testing.T) {
	instance := ParseInstance("8.0.36 MySQL Community Server - GPL")
	if !instance.Parsed {
		t.Fatal("expected mysql version to parse")
	}
	if instance.Engine != EngineMySQL {
		t.Fatalf("expected mysql engine, got %q", instance.Engine)
	}
	if instance.Version != "8.0.36" {
		t.Fatalf("unexpected parsed version: %q", instance.Version)
	}
}

func TestParseInstanceMariaDB(t *testing.T) {
	instance := ParseInstance("10.11.7-MariaDB-1:10.11.7+maria~ubu2204")
	if !instance.Parsed {
		t.Fatal("expected mariadb version to parse")
	}
	if instance.Engine != EngineMariaDB {
		t.Fatalf("expected mariadb engine, got %q", instance.Engine)
	}
}

func TestEvaluateSameEngineMajorGapIncompatible(t *testing.T) {
	source := ParseInstance("8.0.36 MySQL Community Server - GPL")
	dest := ParseInstance("5.7.44 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil)
	if report.Compatible {
		t.Fatal("expected incompatible report for large major downgrade gap")
	}
	if !report.Downgrade {
		t.Fatal("expected downgrade flag true")
	}
}

func TestEvaluateCrossEngineDowngradeThreshold(t *testing.T) {
	source := ParseInstance("8.0.36 MySQL Community Server - GPL")
	dest := ParseInstance("10.5.22-MariaDB")
	report := Evaluate(source, dest, nil)
	if report.Compatible {
		t.Fatal("expected incompatible report for mysql8 -> mariadb10.5")
	}
}

func TestEvaluatePartialScopeInfoFinding(t *testing.T) {
	source := ParseInstance("10.11.7-MariaDB")
	dest := ParseInstance("10.11.5-MariaDB")
	report := Evaluate(source, dest, []string{"db1", "db2"})
	found := false
	for _, finding := range report.Findings {
		if finding.Code == "partial_database_scope" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected partial scope finding")
	}
}
