package config

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileConfigYAML(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cfg.yaml")
	content := []byte("source: mysql://src\ndest: mysql://dst\nconcurrency: 6\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, err := LoadFileConfig(path)
	if err != nil {
		t.Fatalf("expected load success: %v", err)
	}
	if cfg.Source == nil || *cfg.Source != "mysql://src" {
		t.Fatalf("unexpected source: %#v", cfg.Source)
	}
	if cfg.Concurrency == nil || *cfg.Concurrency != 6 {
		t.Fatalf("unexpected concurrency: %#v", cfg.Concurrency)
	}
}

func TestLoadFileConfigDowngradeProfile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cfg.yaml")
	content := []byte("downgrade-profile: max-compat\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, err := LoadFileConfig(path)
	if err != nil {
		t.Fatalf("expected load success: %v", err)
	}
	if cfg.DowngradeProfile == nil || *cfg.DowngradeProfile != "max-compat" {
		t.Fatalf("unexpected downgrade-profile: %#v", cfg.DowngradeProfile)
	}
}

func TestMergeFileConfigRespectsExplicitFlags(t *testing.T) {
	source := "mysql://from-file"
	concurrency := 9
	profile := "max-compat"
	fileCfg := FileConfig{Source: &source, Concurrency: &concurrency, DowngradeProfile: &profile}
	target := RuntimeConfig{Source: "mysql://from-flag", Concurrency: 2, DowngradeProfile: "strict-lts"}
	explicit := map[string]struct{}{"source": {}}

	MergeFileConfig(&target, fileCfg, explicit)

	if target.Source != "mysql://from-flag" {
		t.Fatalf("explicit flag should win, got source=%q", target.Source)
	}
	if target.Concurrency != 9 {
		t.Fatalf("file config should apply to non-explicit field, got %d", target.Concurrency)
	}
	if target.DowngradeProfile != "max-compat" {
		t.Fatalf("file config should apply downgrade profile, got %q", target.DowngradeProfile)
	}
}

func TestLoadFileConfigJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "cfg.json")
	content := []byte(`{"source":"mysql://src","dest":"mysql://dst","concurrency":5}`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	cfg, err := LoadFileConfig(path)
	if err != nil {
		t.Fatalf("expected load success: %v", err)
	}
	if cfg.Concurrency == nil || *cfg.Concurrency != 5 {
		t.Fatalf("unexpected concurrency: %#v", cfg.Concurrency)
	}
}

func TestCollectSetFlags(t *testing.T) {
	var cfg RuntimeConfig
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	BindGlobalFlags(fs, &cfg)
	if err := fs.Parse([]string{"--source", "mysql://src", "--json"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	set := CollectSetFlags(fs)
	if _, ok := set["source"]; !ok {
		t.Fatal("expected source to be marked as explicit")
	}
	if _, ok := set["json"]; !ok {
		t.Fatal("expected json to be marked as explicit")
	}
	if _, ok := set["dest"]; ok {
		t.Fatal("did not expect dest to be marked explicit")
	}
}
