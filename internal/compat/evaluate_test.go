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
	report := Evaluate(source, dest, nil, "strict-lts")
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
	report := Evaluate(source, dest, nil, "strict-lts")
	if report.Compatible {
		t.Fatal("expected incompatible report for mysql8 -> mariadb10.5")
	}
}

func TestEvaluatePartialScopeInfoFinding(t *testing.T) {
	source := ParseInstance("10.11.7-MariaDB")
	dest := ParseInstance("10.11.5-MariaDB")
	report := Evaluate(source, dest, []string{"db1", "db2"}, "strict-lts")
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

func TestEvaluateDefaultsToStrictLTS(t *testing.T) {
	source := ParseInstance("8.0.36 MySQL Community Server - GPL")
	dest := ParseInstance("8.0.20 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "")
	if report.DowngradeProfile != "strict-lts" {
		t.Fatalf("expected strict-lts default, got %q", report.DowngradeProfile)
	}
}

func TestEvaluateSameMajorProfileAllowsSameMajorDowngrade(t *testing.T) {
	source := ParseInstance("8.4.2 MySQL Community Server - GPL")
	dest := ParseInstance("8.0.39 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "same-major")
	if !report.Compatible {
		t.Fatalf("expected compatible same-major downgrade, findings=%#v", report.Findings)
	}
}

func TestEvaluateAdjacentMinorProfileRejectsNonAdjacentDowngrade(t *testing.T) {
	source := ParseInstance("8.4.2 MySQL Community Server - GPL")
	dest := ParseInstance("8.1.39 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "adjacent-minor")
	if report.Compatible {
		t.Fatalf("expected incompatible adjacent-minor downgrade with large gap, findings=%#v", report.Findings)
	}
	if !hasFinding(report.Findings, "downgrade_minor_gap") {
		t.Fatalf("expected downgrade_minor_gap finding, got %#v", report.Findings)
	}
}

func TestEvaluateMaxCompatAllowsLargeGapDowngrade(t *testing.T) {
	source := ParseInstance("8.0.36 MySQL Community Server - GPL")
	dest := ParseInstance("5.7.44 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "max-compat")
	if !report.Compatible {
		t.Fatalf("expected max-compat to allow large gap downgrade, findings=%#v", report.Findings)
	}
	if !hasFinding(report.Findings, "max_compat_profile") {
		t.Fatalf("expected max_compat_profile finding, got %#v", report.Findings)
	}
}

func TestEvaluateStrictLTSRequiresKnownLTSLine(t *testing.T) {
	source := ParseInstance("8.2.5 MySQL Community Server - GPL")
	dest := ParseInstance("8.1.9 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "strict-lts")
	if report.Compatible {
		t.Fatalf("expected strict-lts unknown line to be incompatible, findings=%#v", report.Findings)
	}
	if !hasFinding(report.Findings, "strict_lts_matrix_out_of_range") {
		t.Fatalf("expected strict_lts_matrix_out_of_range finding, got %#v", report.Findings)
	}
}

func TestEvaluateStrictLTSBlocksCrossLineDowngrade(t *testing.T) {
	source := ParseInstance("8.4.2 MySQL Community Server - GPL")
	dest := ParseInstance("8.0.39 MySQL Community Server - GPL")
	report := Evaluate(source, dest, nil, "strict-lts")
	if report.Compatible {
		t.Fatalf("expected strict-lts cross-line downgrade to be incompatible, findings=%#v", report.Findings)
	}
	if !hasFinding(report.Findings, "strict_lts_matrix_mismatch") {
		t.Fatalf("expected strict_lts_matrix_mismatch finding, got %#v", report.Findings)
	}
}

func TestEvaluateStrictLTSAllowsSameLineDowngrade(t *testing.T) {
	source := ParseInstance("10.11.8-MariaDB")
	dest := ParseInstance("10.11.5-MariaDB")
	report := Evaluate(source, dest, nil, "strict-lts")
	if !report.Compatible {
		t.Fatalf("expected strict-lts same-line downgrade to be compatible, findings=%#v", report.Findings)
	}
	if !hasFinding(report.Findings, "strict_lts_matrix_match") {
		t.Fatalf("expected strict_lts_matrix_match finding, got %#v", report.Findings)
	}
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
