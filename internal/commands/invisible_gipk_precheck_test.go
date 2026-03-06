package commands

import (
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
)

func TestDestinationSupportsInvisibleColumns(t *testing.T) {
	if !destinationSupportsInvisibleColumns(compat.ParseInstance("8.0.45 MySQL Community Server - GPL")) {
		t.Fatal("expected MySQL 8.0.45 to support invisible columns")
	}
	if destinationSupportsInvisibleColumns(compat.ParseInstance("8.0.22 MySQL Community Server - GPL")) {
		t.Fatal("did not expect MySQL 8.0.22 to support invisible columns")
	}
	if destinationSupportsInvisibleColumns(compat.ParseInstance("11.0.6-MariaDB-ubu2204")) {
		t.Fatal("did not expect MariaDB to preserve MySQL invisible-column semantics")
	}
}

func TestDestinationSupportsGIPK(t *testing.T) {
	if !destinationSupportsGIPK(compat.ParseInstance("8.0.45 MySQL Community Server - GPL")) {
		t.Fatal("expected MySQL 8.0.45 to support GIPK")
	}
	if destinationSupportsGIPK(compat.ParseInstance("8.0.29 MySQL Community Server - GPL")) {
		t.Fatal("did not expect MySQL 8.0.29 to support GIPK")
	}
	if destinationSupportsGIPK(compat.ParseInstance("11.0.6-MariaDB-ubu2204")) {
		t.Fatal("did not expect MariaDB to preserve MySQL GIPK semantics")
	}
}

func TestBuildInvisibleGIPKFindingsMySQLToMariaDB(t *testing.T) {
	report := invisibleGIPKPrecheckReport{
		InvisibleColumnCount: 1,
		InvisibleIndexCount:  1,
		GIPKTableCount:       1,
		InvisibleColumns: []invisibleColumnIssue{{
			Database: "app",
			Table:    "invisible_demo",
			Column:   "secret_token",
			Extra:    "INVISIBLE",
			Proposal: "materialize it",
		}},
		InvisibleIndexes: []invisibleIndexIssue{{
			Database: "app",
			Table:    "invisible_demo",
			Index:    "idx_secret_token",
			Proposal: "make it visible",
		}},
		GIPKTables: []generatedInvisiblePrimaryKeyIssue{{
			Database: "app",
			Table:    "gipk_demo",
			Column:   "my_row_id",
			Proposal: "choose include or skip",
		}},
	}

	source := compat.ParseInstance("8.4.8 MySQL Community Server - GPL")
	dest := compat.ParseInstance("11.0.6-MariaDB-ubu2204")

	findings := buildInvisibleGIPKFindings(source, dest, report)
	if len(findings) != 4 {
		t.Fatalf("expected 4 findings, got %d", len(findings))
	}
	if findings[0].Code != "invisible_gipk_features_detected" || findings[0].Severity != "error" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if !strings.Contains(findings[0].Proposal, "MariaDB does not preserve") {
		t.Fatalf("unexpected summary proposal: %q", findings[0].Proposal)
	}
	if !invisibleGIPKIncompatible(source, dest, report) {
		t.Fatal("expected mysql->mariadb hidden-schema path to be incompatible")
	}
}

func TestBuildInvisibleGIPKFindingsMySQLToMySQLCompatible(t *testing.T) {
	report := invisibleGIPKPrecheckReport{
		InvisibleColumnCount: 1,
		GIPKTableCount:       1,
		InvisibleColumns: []invisibleColumnIssue{{
			Database: "app",
			Table:    "invisible_demo",
			Column:   "secret_token",
			Extra:    "INVISIBLE",
			Proposal: "materialize it",
		}},
		GIPKTables: []generatedInvisiblePrimaryKeyIssue{{
			Database: "app",
			Table:    "gipk_demo",
			Column:   "my_row_id",
			Proposal: "choose include or skip",
		}},
	}

	source := compat.ParseInstance("8.4.8 MySQL Community Server - GPL")
	dest := compat.ParseInstance("8.0.45 MySQL Community Server - GPL")

	findings := buildInvisibleGIPKFindings(source, dest, report)
	if len(findings) != 3 {
		t.Fatalf("expected 3 findings, got %d", len(findings))
	}
	if findings[0].Severity != "warn" {
		t.Fatalf("expected summary severity warn, got %#v", findings[0])
	}
	if !strings.Contains(findings[0].Proposal, "dump/import mode explicit") {
		t.Fatalf("expected GIPK proposal in summary, got %q", findings[0].Proposal)
	}
	if invisibleGIPKIncompatible(source, dest, report) {
		t.Fatal("did not expect mysql84->mysql80 hidden-schema path to be incompatible")
	}
}

func TestBuildInvisibleGIPKFindingsClean(t *testing.T) {
	findings := buildInvisibleGIPKFindings(
		compat.ParseInstance("8.4.8 MySQL Community Server - GPL"),
		compat.ParseInstance("8.0.45 MySQL Community Server - GPL"),
		invisibleGIPKPrecheckReport{},
	)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != "invisible_gipk_inventory_clean" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}
