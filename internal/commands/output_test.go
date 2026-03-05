package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/esthergb/dbmigrate/internal/config"
)

func TestWriteResultJSONStatus(t *testing.T) {
	var out bytes.Buffer
	if err := writeResult(&out, config.RuntimeConfig{JSON: true}, "migrate", "ok", "done"); err != nil {
		t.Fatalf("writeResult json: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, `"status": "ok"`) {
		t.Fatalf("expected status ok in json output, got %q", text)
	}
	if !strings.Contains(text, `"command": "migrate"`) {
		t.Fatalf("expected command migrate in json output, got %q", text)
	}
}

func TestWriteResultTextStatus(t *testing.T) {
	var out bytes.Buffer
	if err := writeResult(&out, config.RuntimeConfig{}, "plan", "dry-run", "test message"); err != nil {
		t.Fatalf("writeResult text: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "[plan] status=dry-run") {
		t.Fatalf("expected text output with status, got %q", text)
	}
}
