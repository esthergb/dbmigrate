package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
)

func TestParseMigrateOptionsDefaults(t *testing.T) {
	opts, err := parseMigrateOptions(nil)
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.DestEmptyRequired != true {
		t.Fatalf("expected dest-empty-required default true, got %v", opts.DestEmptyRequired)
	}
	if opts.ChunkSize != 1000 {
		t.Fatalf("expected default chunk size 1000, got %d", opts.ChunkSize)
	}
}

func TestParseMigrateOptionsExplicit(t *testing.T) {
	opts, err := parseMigrateOptions([]string{"--schema-only", "--force", "--dest-empty-required=false", "--chunk-size=250", "--resume"})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if !opts.SchemaOnly {
		t.Fatal("expected schema-only=true")
	}
	if !opts.Force {
		t.Fatal("expected force=true")
	}
	if opts.DestEmptyRequired {
		t.Fatal("expected dest-empty-required=false")
	}
	if opts.ChunkSize != 250 {
		t.Fatalf("expected chunk size 250, got %d", opts.ChunkSize)
	}
	if !opts.Resume {
		t.Fatal("expected resume=true")
	}
}

func TestParseMigrateOptionsInvalidChunk(t *testing.T) {
	_, err := parseMigrateOptions([]string{"--chunk-size=0"})
	if err == nil {
		t.Fatal("expected parse error for invalid chunk-size")
	}
}

func TestHasObject(t *testing.T) {
	if !hasObject([]string{"tables", "views"}, "views") {
		t.Fatal("expected views to be included")
	}
	if hasObject([]string{"tables"}, "triggers") {
		t.Fatal("did not expect triggers to be included")
	}
}

func TestSandboxDatabaseNameLength(t *testing.T) {
	name := sandboxDatabaseName("abc123", "this-is-a-very-long-database-name-with-special-characters-and-length")
	if len(name) > 64 {
		t.Fatalf("expected sandbox database name length <= 64, got %d (%q)", len(name), name)
	}
}

func TestWriteMigrateDryRunSandboxReportText(t *testing.T) {
	report := migrateDryRunSandboxResult{
		Command:       "migrate",
		Status:        "dry-run",
		DryRunMode:    "sandbox",
		Validated:     12,
		Failed:        1,
		CleanupStatus: "ok",
		Message:       "validation failed",
	}
	var out bytes.Buffer
	if err := writeMigrateDryRunSandboxReport(&out, config.RuntimeConfig{}, report); err != nil {
		t.Fatalf("write text report: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "validated=12") || !strings.Contains(text, "cleanup_status=ok") {
		t.Fatalf("unexpected report text: %q", text)
	}
	if !strings.Contains(text, "detail=validation failed") {
		t.Fatalf("expected detail line in report text, got %q", text)
	}
}

func TestWriteMigratePrecheckReportText(t *testing.T) {
	report := zeroDateDefaultsPrecheckReport{
		Name:                "zero-date-defaults",
		Incompatible:        true,
		DestinationSQLMode:  "STRICT_TRANS_TABLES,NO_ZERO_DATE,NO_ZERO_IN_DATE",
		DestinationEnforced: true,
		FixScriptPath:       "./state/precheck-zero-date-fixes.sql",
		IssueCount:          1,
		Findings: []compat.Finding{
			{
				Code:     "zero_date_default_column",
				Severity: "error",
				Message:  "test message",
				Proposal: "test proposal",
			},
		},
	}
	var out bytes.Buffer
	if err := writeMigratePrecheckReport(&out, config.RuntimeConfig{}, report); err != nil {
		t.Fatalf("write text report: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "status=incompatible") || !strings.Contains(text, "precheck=zero-date-defaults") {
		t.Fatalf("unexpected report text: %q", text)
	}
	if !strings.Contains(text, "fix_script=\"./state/precheck-zero-date-fixes.sql\"") {
		t.Fatalf("expected fix script path in precheck output: %q", text)
	}
	if !strings.Contains(text, "code=zero_date_default_column") {
		t.Fatalf("expected finding line in report text: %q", text)
	}
}
