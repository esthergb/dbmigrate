package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/config"
	schemaVerify "github.com/esthergb/dbmigrate/internal/verify/schema"
)

func TestParseVerifyOptionsDefaults(t *testing.T) {
	opts, err := parseVerifyOptions(nil)
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.VerifyLevel != "schema" {
		t.Fatalf("expected default verify-level schema, got %q", opts.VerifyLevel)
	}
}

func TestParseVerifyOptionsExplicit(t *testing.T) {
	opts, err := parseVerifyOptions([]string{"--verify-level=data"})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.VerifyLevel != "data" {
		t.Fatalf("expected verify-level data, got %q", opts.VerifyLevel)
	}
}

func TestParseVerifyOptionsInvalid(t *testing.T) {
	_, err := parseVerifyOptions([]string{"--verify-level=full"})
	if err == nil {
		t.Fatal("expected parse error for invalid verify-level")
	}
}

func TestWriteVerifyResultText(t *testing.T) {
	var out bytes.Buffer
	summary := schemaVerify.Summary{
		Databases:            1,
		ObjectsCompared:      2,
		MissingInDestination: 1,
		MissingInSource:      0,
		DefinitionMismatches: 1,
		Diffs: []schemaVerify.Diff{
			{
				Kind:       "definition_mismatch",
				Database:   "app",
				ObjectType: "table",
				ObjectName: "users",
			},
		},
	}

	if err := writeVerifyResult(&out, config.RuntimeConfig{}, "schema", summary); err != nil {
		t.Fatalf("write verify result text: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "status=diff") {
		t.Fatalf("expected diff status in output, got %q", text)
	}
	if !strings.Contains(text, "object=table:users") {
		t.Fatalf("expected diff object in output, got %q", text)
	}
}

func TestWriteVerifyResultJSON(t *testing.T) {
	var out bytes.Buffer
	cfg := config.RuntimeConfig{JSON: true}

	if err := writeVerifyResult(&out, cfg, "schema", schemaVerify.Summary{}); err != nil {
		t.Fatalf("write verify result json: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "\"verify_level\": \"schema\"") {
		t.Fatalf("expected verify_level in json output, got %q", text)
	}
	if !strings.Contains(text, "\"status\": \"ok\"") {
		t.Fatalf("expected ok status in json output, got %q", text)
	}
}
