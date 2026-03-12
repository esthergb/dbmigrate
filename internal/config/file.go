package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileConfig defines optional runtime settings loaded from YAML/JSON.
type FileConfig struct {
	Source           *string  `json:"source" yaml:"source"`
	Dest             *string  `json:"dest" yaml:"dest"`
	Databases        []string `json:"databases" yaml:"databases"`
	ExcludeDatabases []string `json:"exclude_databases" yaml:"exclude-databases"`
	IncludeObjects   []string `json:"include_objects" yaml:"include-objects"`
	Concurrency      *int     `json:"concurrency" yaml:"concurrency"`
	RateLimit        *int     `json:"rate_limit" yaml:"rate-limit"`
	DryRun           *bool    `json:"dry_run" yaml:"dry-run"`
	DryRunMode       *string  `json:"dry_run_mode" yaml:"dry-run-mode"`
	Verbose          *bool    `json:"verbose" yaml:"verbose"`
	JSON             *bool    `json:"json" yaml:"json"`
	TLSMode          *string  `json:"tls_mode" yaml:"tls-mode"`
	CAFile           *string  `json:"ca_file" yaml:"ca-file"`
	CertFile         *string  `json:"cert_file" yaml:"cert-file"`
	KeyFile          *string  `json:"key_file" yaml:"key-file"`
	OperationTimeout *string  `json:"operation_timeout" yaml:"operation-timeout"`
	StateDir         *string  `json:"state_dir" yaml:"state-dir"`
	DowngradeProfile *string  `json:"downgrade_profile" yaml:"downgrade-profile"`
}

// LoadFileConfig reads YAML/JSON runtime configuration from disk.
func LoadFileConfig(path string) (FileConfig, error) {
	if strings.TrimSpace(path) == "" {
		return FileConfig{}, errors.New("config path is empty")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read config file: %w", err)
	}

	var out FileConfig
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&out); err == nil {
			var trailing any
			trailingErr := dec.Decode(&trailing)
			if trailingErr != io.EOF {
				if trailingErr == nil {
					err = errors.New("unexpected trailing JSON content")
				} else {
					err = trailingErr
				}
			}
		}
	case ".yaml", ".yml", "":
		dec := yaml.NewDecoder(bytes.NewReader(raw))
		dec.KnownFields(true)
		if err = dec.Decode(&out); err == nil {
			var trailing any
			trailingErr := dec.Decode(&trailing)
			if trailingErr != io.EOF {
				if trailingErr == nil {
					err = errors.New("unexpected trailing YAML content")
				} else {
					err = trailingErr
				}
			}
		}
	default:
		return FileConfig{}, fmt.Errorf("unsupported config extension %q", ext)
	}
	if err != nil {
		return FileConfig{}, fmt.Errorf("parse config file: %w", err)
	}
	if out.OperationTimeout != nil {
		if _, err := parseOperationTimeout(*out.OperationTimeout); err != nil {
			return FileConfig{}, fmt.Errorf("parse config file: invalid operation-timeout: %w", err)
		}
	}

	return out, nil
}

// MergeFileConfig applies file config values unless overridden by explicit CLI flags.
func MergeFileConfig(target *RuntimeConfig, fileCfg FileConfig, explicit map[string]struct{}) {
	if target == nil {
		return
	}

	if _, ok := explicit["source"]; !ok && fileCfg.Source != nil {
		target.Source = *fileCfg.Source
	}
	if _, ok := explicit["dest"]; !ok && fileCfg.Dest != nil {
		target.Dest = *fileCfg.Dest
	}
	if _, ok := explicit["databases"]; !ok && fileCfg.Databases != nil {
		target.Databases = cloneList(fileCfg.Databases)
		target.databasesRaw = ""
	}
	if _, ok := explicit["exclude-databases"]; !ok && fileCfg.ExcludeDatabases != nil {
		target.ExcludeDatabases = cloneList(fileCfg.ExcludeDatabases)
		target.excludeDatabasesRaw = ""
	}
	if _, ok := explicit["include-objects"]; !ok && fileCfg.IncludeObjects != nil {
		target.IncludeObjects = cloneList(fileCfg.IncludeObjects)
		target.includeObjectsRaw = ""
	}
	if _, ok := explicit["concurrency"]; !ok && fileCfg.Concurrency != nil {
		target.Concurrency = *fileCfg.Concurrency
	}
	if _, ok := explicit["rate-limit"]; !ok && fileCfg.RateLimit != nil {
		target.RateLimit = *fileCfg.RateLimit
	}
	if _, ok := explicit["dry-run"]; !ok && fileCfg.DryRun != nil {
		target.DryRun = *fileCfg.DryRun
	}
	if _, ok := explicit["dry-run-mode"]; !ok && fileCfg.DryRunMode != nil {
		target.DryRunMode = *fileCfg.DryRunMode
	}
	if _, ok := explicit["verbose"]; !ok && fileCfg.Verbose != nil {
		target.Verbose = *fileCfg.Verbose
	}
	if _, ok := explicit["json"]; !ok && fileCfg.JSON != nil {
		target.JSON = *fileCfg.JSON
	}
	if _, ok := explicit["tls-mode"]; !ok && fileCfg.TLSMode != nil {
		target.TLSMode = *fileCfg.TLSMode
	}
	if _, ok := explicit["ca-file"]; !ok && fileCfg.CAFile != nil {
		target.CAFile = *fileCfg.CAFile
	}
	if _, ok := explicit["cert-file"]; !ok && fileCfg.CertFile != nil {
		target.CertFile = *fileCfg.CertFile
	}
	if _, ok := explicit["key-file"]; !ok && fileCfg.KeyFile != nil {
		target.KeyFile = *fileCfg.KeyFile
	}
	if _, ok := explicit["operation-timeout"]; !ok && fileCfg.OperationTimeout != nil {
		duration, _ := parseOperationTimeout(*fileCfg.OperationTimeout)
		target.OperationTimeout = duration
	}
	if _, ok := explicit["state-dir"]; !ok && fileCfg.StateDir != nil {
		target.StateDir = *fileCfg.StateDir
	}
	if _, ok := explicit["downgrade-profile"]; !ok && fileCfg.DowngradeProfile != nil {
		target.DowngradeProfile = *fileCfg.DowngradeProfile
	}
}

func parseOperationTimeout(raw string) (time.Duration, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, errors.New("operation-timeout is empty")
	}
	return time.ParseDuration(trimmed)
}

func cloneList(items []string) []string {
	if items == nil {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}
