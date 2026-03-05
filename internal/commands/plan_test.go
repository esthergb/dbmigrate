package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/compat"
	"github.com/esthergb/dbmigrate/internal/config"
)

func TestWritePlanReportText(t *testing.T) {
	report := compat.Report{
		Compatible:       false,
		Downgrade:        true,
		RequiresEvidence: true,
		Source:           compat.ParseInstance("8.0.36 MySQL Community Server - GPL"),
		Dest:             compat.ParseInstance("5.7.44 MySQL Community Server - GPL"),
		Findings: []compat.Finding{
			{
				Code:     "downgrade_major_gap",
				Severity: "error",
				Message:  "test message",
				Proposal: "test proposal",
			},
		},
	}

	var out bytes.Buffer
	if err := writePlanReport(&out, config.RuntimeConfig{}, report); err != nil {
		t.Fatalf("write plan report text: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "status=incompatible") {
		t.Fatalf("expected incompatible status in output, got %q", text)
	}
	if !strings.Contains(text, "code=downgrade_major_gap") {
		t.Fatalf("expected finding code in output, got %q", text)
	}
	if !strings.Contains(text, "requires_evidence=true") {
		t.Fatalf("expected requires_evidence flag in output, got %q", text)
	}
}

func TestWritePlanReportJSON(t *testing.T) {
	report := compat.Report{
		Compatible: true,
		Downgrade:  false,
		Source:     compat.ParseInstance("10.11.7-MariaDB"),
		Dest:       compat.ParseInstance("10.11.8-MariaDB"),
	}

	var out bytes.Buffer
	if err := writePlanReport(&out, config.RuntimeConfig{JSON: true}, report); err != nil {
		t.Fatalf("write plan report json: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "\"status\": \"compatible\"") {
		t.Fatalf("expected compatible status in json, got %q", text)
	}
	if !strings.Contains(text, "\"command\": \"plan\"") {
		t.Fatalf("expected plan command in json, got %q", text)
	}
	if !strings.Contains(text, "\"requires_evidence\": false") {
		t.Fatalf("expected requires_evidence field in json, got %q", text)
	}
}
