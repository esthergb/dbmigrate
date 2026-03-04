package config

import (
	"flag"
	"testing"
)

func TestRuntimeConfigValidation(t *testing.T) {
	cfg := RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "strict-lts"}
	if err := cfg.ValidateBasic(); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	cfg.Concurrency = 0
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected concurrency validation error")
	}

	cfg = RuntimeConfig{TLSMode: "required", Concurrency: 2, DowngradeProfile: "invalid"}
	if err := cfg.ValidateBasic(); err == nil {
		t.Fatal("expected downgrade-profile validation error")
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
		"--tls-mode", "preferred",
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
	if cfg.DowngradeProfile != "strict-lts" {
		t.Fatalf("expected default downgrade-profile strict-lts, got %q", cfg.DowngradeProfile)
	}
	if len(cfg.ExcludeDatabases) == 0 {
		t.Fatal("expected default excluded databases")
	}
}
