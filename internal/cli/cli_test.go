package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

func TestRunPlanWithConfigFile(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "dbmigrate.yaml")
	cfg := []byte("source: mysql://cfg-src\ndest: mysql://cfg-dst\njson: true\n")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	var out bytes.Buffer
	args := []string{"plan", "--config", cfgPath}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
	if out.Len() == 0 {
		t.Fatal("expected command output")
	}
}

func TestRunPlanFlagOverridesConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "dbmigrate.yaml")
	cfg := []byte("source: mysql://cfg-src\ndest: mysql://cfg-dst\n")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	var out bytes.Buffer
	args := []string{
		"plan",
		"--config", cfgPath,
		"--source", "mysql://flag-src",
		"--dest", "mysql://flag-dst",
	}
	code := Run(context.Background(), args, &out, &out)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d output=%s", code, out.String())
	}
}
