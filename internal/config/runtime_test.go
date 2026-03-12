package config

import (
	"flag"
	"testing"
	"time"
)

func TestRuntimeConfigValidation(t *testing.T) {
	cfg := RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "plan"}
	if err := cfg.ValidateBasic(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	cfg.Concurrency = 0
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected concurrency validation error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "invalid", DryRunMode: "plan"}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected downgrade-profile validation error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "unknown"}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected dry-run-mode validation error")
	}

	cfg = RuntimeConfig{
		TLSMode:          "required",
		Concurrency:      2,
		DowngradeProfile: "strict-lts",
		DryRunMode:       "plan",
		Source:           "user:pass@tcp(localhost:3306)/app?tls=preferred",
		Dest:             "mysql://user:pass@localhost:3306/app",
	}
	if err := cfg.ValidateBasic(); err != nil {
		t.Fatalf("expected driver-style DSN to be accepted, got error: %v", err)
	}

	cfg = RuntimeConfig{
		TLSMode:          "required",
		Concurrency:      2,
		DowngradeProfile: "strict-lts",
		DryRunMode:       "plan",
		Source:           "postgres://localhost:5432/app",
	}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected invalid source DSN format error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "plan", OperationTimeout: -1 * time.Second}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected operation-timeout validation error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "plan", RateLimit: -1}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected rate-limit validation error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "plan", RateLimit: 0}
	if err := cfg.ValidateBasic(); err != nil {
		t.Fatalf("expected rate-limit=0 to be valid, got: %v", err)
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts", DryRunMode: "plan", RateLimit: 1000}
	if err := cfg.ValidateBasic(); err != nil {
		t.Fatalf("expected rate-limit=1000 to be valid, got: %v", err)
	}
}

func TestBindGlobalFlagsAndFinalize(t *testing.T) {
	var cfg RuntimeConfig
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	BindGlobalFlags(fs, &cfg)

	args := []string{
		"--source", "mysql://src",
		"--dest", "mysql://dst",
		"--concurrency", "8",
		"--rate-limit", "5000",
	}
	if err := fs.Parse(args); err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	cfg.Finalize()

	if cfg.Source != "mysql://src" || cfg.Dest != "mysql://dst" {
		t.Fatalf("unexpected source/dest values: %#v", cfg)
	}
	if cfg.Concurrency != 8 {
		t.Fatalf("expected concurrency=8, got %d", cfg.Concurrency)
	}
	if cfg.RateLimit != 5000 {
		t.Fatalf("expected rate-limit=5000, got %d", cfg.RateLimit)
	}
	if cfg.DowngradeProfile != "strict-lts" {
		t.Fatalf("expected default downgrade-profile strict-lts, got %q", cfg.DowngradeProfile)
	}
	if cfg.DryRunMode != "plan" {
		t.Fatalf("expected default dry-run-mode plan, got %q", cfg.DryRunMode)
	}
	if len(cfg.ExcludeDatabases) == 0 {
		t.Fatal("expected default excluded databases")
	}
	if cfg.TLSMode != "required" {
		t.Fatalf("expected default tls-mode required, got %q", cfg.TLSMode)
	}
	if cfg.OperationTimeout != 0 {
		t.Fatalf("expected default operation-timeout 0, got %s", cfg.OperationTimeout)
	}
}
