package commands

import (
	"strings"
	"testing"
)

func TestCollationHasClientCompatibilityRisk(t *testing.T) {
	if !collationHasClientCompatibilityRisk("utf8mb4_uca1400_ai_ci") {
		t.Fatal("expected utf8mb4_uca1400_ai_ci to be a client compatibility risk")
	}
	if !collationHasClientCompatibilityRisk("uca1400_ai_ci") {
		t.Fatal("expected uca1400_ai_ci to be a client compatibility risk")
	}
	if collationHasClientCompatibilityRisk("utf8mb4_0900_ai_ci") {
		t.Fatal("did not expect utf8mb4_0900_ai_ci to be client compatibility risk")
	}
}

func TestDetectUnsupportedDestinationCollations(t *testing.T) {
	sourceItems := []collationIssue{
		{Scope: "schema", Database: "app", Collation: "utf8mb4_0900_ai_ci"},
		{Scope: "column", Database: "app", Table: "messages", Column: "body", Collation: "utf8mb4_uca1400_ai_ci"},
	}
	supported := map[string]struct{}{
		"utf8mb4_unicode_ci":    {},
		"utf8mb4_uca1400_ai_ci": {},
		"uca1400_ai_ci":         {},
	}

	issues := detectUnsupportedDestinationCollations(sourceItems, supported)
	if len(issues) != 1 {
		t.Fatalf("expected 1 unsupported destination collation, got %d", len(issues))
	}
	if issues[0].Collation != "utf8mb4_0900_ai_ci" {
		t.Fatalf("unexpected unsupported collation: %#v", issues[0])
	}
}

func TestBuildCollationPrecheckFindingsSeparatesServerUnsupportedFromClientRisk(t *testing.T) {
	report := collationPrecheckReport{
		UnsupportedDestinationCount:  1,
		ClientCompatibilityRiskCount: 1,
		UnsupportedDestination: []collationIssue{{
			Scope:     "column",
			Database:  "app",
			Table:     "messages",
			Column:    "body",
			Collation: "utf8mb4_0900_ai_ci",
			Proposal:  "map it",
		}},
		ClientCompatibilityRisks: []collationIssue{{
			Scope:     "schema",
			Database:  "app",
			Collation: "utf8mb4_uca1400_ai_ci",
			Proposal:  "rehearse clients",
		}},
	}

	findings := buildCollationPrecheckFindings(report)
	if len(findings) != 4 {
		t.Fatalf("expected 4 findings, got %d", len(findings))
	}
	if findings[0].Code != "unsupported_destination_collations_detected" || findings[0].Severity != "error" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if findings[2].Code != "client_collation_compatibility_risk_detected" || findings[2].Severity != "warn" {
		t.Fatalf("unexpected client risk summary: %#v", findings[2])
	}
	if !strings.Contains(findings[0].Proposal, "server-side incompatibility") {
		t.Fatalf("unexpected unsupported proposal: %q", findings[0].Proposal)
	}
}

func TestBuildCollationPrecheckFindingsClean(t *testing.T) {
	findings := buildCollationPrecheckFindings(collationPrecheckReport{})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Code != "collation_inventory_clean" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}
