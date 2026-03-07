package commands

import (
	"strings"
	"testing"
)

func TestDetectForeignKeyCyclesFindsStronglyConnectedGroups(t *testing.T) {
	dependencies := map[string]map[string]struct{}{
		"a": {"b": {}},
		"b": {"c": {}},
		"c": {"a": {}},
		"d": {"e": {}},
		"e": {"d": {}},
		"f": {},
	}

	cycles := detectForeignKeyCycles(dependencies)
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycle groups, got %#v", cycles)
	}
	if strings.Join(cycles[0], ",") != "a,b,c" {
		t.Fatalf("unexpected first cycle group: %#v", cycles[0])
	}
	if strings.Join(cycles[1], ",") != "d,e" {
		t.Fatalf("unexpected second cycle group: %#v", cycles[1])
	}
}

func TestBuildForeignKeyCycleFindings(t *testing.T) {
	report := foreignKeyCyclePrecheckReport{
		Name:       "foreign-key-cycles",
		IssueCount: 1,
		Issues: []foreignKeyCycleIssue{{
			Database: "app",
			Tables:   []string{"orders", "users"},
		}},
	}

	findings := buildForeignKeyCycleFindings(report)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %#v", findings)
	}
	if findings[0].Code != "foreign_key_cycles_detected" || findings[0].Severity != "error" {
		t.Fatalf("unexpected summary finding: %#v", findings[0])
	}
	if findings[1].Code != "foreign_key_cycle_group" || !strings.Contains(findings[1].Message, "orders, users") {
		t.Fatalf("unexpected detail finding: %#v", findings[1])
	}
}

func TestBuildForeignKeyCycleFindingsClean(t *testing.T) {
	findings := buildForeignKeyCycleFindings(foreignKeyCyclePrecheckReport{})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %#v", findings)
	}
	if findings[0].Code != "foreign_key_cycle_inventory_clean" {
		t.Fatalf("unexpected finding: %#v", findings[0])
	}
}
