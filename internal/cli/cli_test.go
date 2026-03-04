package cli

import (
	"bytes"
	"context"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), nil, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("expected help output")
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	code := Run(context.Background(), []string{"unknown"}, &out, &out)
	if code != 1 {
		t.Fatalf("expected usage exit code 1, got %d", code)
	}
}

func TestRunPlanJSON(t *testing.T) {
	var out bytes.Buffer
	args := []string{"plan", "--source", "mysql://src", "--dest", "mysql://dst", "--json"}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out.Len() == 0 {
		t.Fatal("expected command output")
	}
}
