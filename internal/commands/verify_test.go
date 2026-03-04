package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/config"
	dataVerify "github.com/esthergb/dbmigrate/internal/verify/data"
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
	if opts.DataMode != "count" {
		t.Fatalf("expected default data-mode count, got %q", opts.DataMode)
	}
	if opts.SampleSize != 1000 {
		t.Fatalf("expected default sample-size 1000, got %d", opts.SampleSize)
	}
}

func TestParseVerifyOptionsExplicit(t *testing.T) {
	opts, err := parseVerifyOptions([]string{"--verify-level=data", "--data-mode=hash", "--sample-size=250"})
	if err != nil {
		t.Fatalf("expected parse success: %v", err)
	}
	if opts.VerifyLevel != "data" {
		t.Fatalf("expected verify-level data, got %q", opts.VerifyLevel)
	}
	if opts.DataMode != "hash" {
		t.Fatalf("expected data-mode hash, got %q", opts.DataMode)
	}
	if opts.SampleSize != 250 {
		t.Fatalf("expected sample-size 250, got %d", opts.SampleSize)
	}
}

func TestParseVerifyOptionsInvalidLevel(t *testing.T) {
	_, err := parseVerifyOptions([]string{"--verify-level=full"})
	if err == nil {
		t.Fatal("expected parse error for invalid verify-level")
	}
}

func TestParseVerifyOptionsInvalidDataMode(t *testing.T) {
	_, err := parseVerifyOptions([]string{"--data-mode=approx"})
	if err == nil {
		t.Fatal("expected parse error for invalid data-mode")
	}
}

func TestParseVerifyOptionsInvalidSampleSize(t *testing.T) {
	_, err := parseVerifyOptions([]string{"--sample-size=0"})
	if err == nil {
		t.Fatal("expected parse error for invalid sample-size")
	}
}

func TestWriteSchemaVerifyResultText(t *testing.T) {
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

	if err := writeSchemaVerifyResult(&out, config.RuntimeConfig{}, "schema", summary); err != nil {
		t.Fatalf("write schema verify result text: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "status=diff") {
		t.Fatalf("expected diff status in output, got %q", text)
	}
	if !strings.Contains(text, "object=table:users") {
		t.Fatalf("expected diff object in output, got %q", text)
	}
}

func TestWriteSchemaVerifyResultJSON(t *testing.T) {
	var out bytes.Buffer
	cfg := config.RuntimeConfig{JSON: true}

	if err := writeSchemaVerifyResult(&out, cfg, "schema", schemaVerify.Summary{}); err != nil {
		t.Fatalf("write schema verify result json: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "\"verify_level\": \"schema\"") {
		t.Fatalf("expected verify_level in json output, got %q", text)
	}
	if !strings.Contains(text, "\"status\": \"ok\"") {
		t.Fatalf("expected ok status in json output, got %q", text)
	}
}

func TestWriteDataVerifyResultText(t *testing.T) {
	var out bytes.Buffer
	summary := dataVerify.Summary{
		Databases:            1,
		TablesCompared:       2,
		MissingInDestination: 1,
		MissingInSource:      1,
		CountMismatches:      1,
		Diffs: []dataVerify.Diff{
			{
				Kind:        "row_count_mismatch",
				Database:    "app",
				Table:       "users",
				SourceCount: 10,
				DestCount:   9,
			},
		},
	}

	if err := writeDataVerifyResult(&out, config.RuntimeConfig{}, "data", "count", summary); err != nil {
		t.Fatalf("write data verify result text: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "data_mode=count") {
		t.Fatalf("expected data mode in output, got %q", text)
	}
	if !strings.Contains(text, "table=users") {
		t.Fatalf("expected table identifier in output, got %q", text)
	}
	if !strings.Contains(text, "hash_mismatches=0") {
		t.Fatalf("expected hash mismatch counter in output, got %q", text)
	}
}

func TestWriteDataVerifyResultJSON(t *testing.T) {
	var out bytes.Buffer
	cfg := config.RuntimeConfig{JSON: true}

	if err := writeDataVerifyResult(&out, cfg, "data", "count", dataVerify.Summary{}); err != nil {
		t.Fatalf("write data verify result json: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "\"verify_level\": \"data\"") {
		t.Fatalf("expected verify_level in json output, got %q", text)
	}
	if !strings.Contains(text, "\"data_mode\": \"count\"") {
		t.Fatalf("expected data_mode in json output, got %q", text)
	}
}

func TestWriteDataVerifyResultTextHashDiff(t *testing.T) {
	var out bytes.Buffer
	summary := dataVerify.Summary{
		Databases:            1,
		TablesCompared:       1,
		MissingInDestination: 0,
		MissingInSource:      0,
		HashMismatches:       1,
		Diffs: []dataVerify.Diff{
			{
				Kind:       "table_hash_mismatch",
				Database:   "app",
				Table:      "users",
				SourceHash: "abc",
				DestHash:   "def",
			},
		},
	}

	if err := writeDataVerifyResult(&out, config.RuntimeConfig{}, "data", "hash", summary); err != nil {
		t.Fatalf("write data verify hash result text: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "data_mode=hash") {
		t.Fatalf("expected hash mode in output, got %q", text)
	}
	if !strings.Contains(text, "source_hash=abc") {
		t.Fatalf("expected source hash in output, got %q", text)
	}
}
