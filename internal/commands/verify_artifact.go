package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	dataVerify "github.com/esthergb/dbmigrate/internal/verify/data"
	"github.com/esthergb/dbmigrate/internal/version"
)

type verifyDataArtifact struct {
	Command     string             `json:"command"`
	Status      string             `json:"status"`
	VerifyLevel string             `json:"verify_level"`
	DataMode    string             `json:"data_mode"`
	Summary     dataVerify.Summary `json:"summary"`
	Timestamp   time.Time          `json:"timestamp"`
	Version     string             `json:"version"`
}

func verifyDataArtifactPath(stateDir string) string {
	baseDir := strings.TrimSpace(stateDir)
	if baseDir == "" {
		baseDir = "./state"
	}
	return filepath.Join(baseDir, "verify-data-report.json")
}

func persistVerifyDataArtifact(stateDir string, dataMode string, summary dataVerify.Summary) error {
	status := "ok"
	if len(summary.Diffs) > 0 {
		status = "diff"
	}
	payload := verifyDataArtifact{
		Command:     "verify",
		Status:      status,
		VerifyLevel: "data",
		DataMode:    dataMode,
		Summary:     summary,
		Timestamp:   time.Now().UTC(),
		Version:     version.Version,
	}

	path := verifyDataArtifactPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir state-dir for verify data artifact: %w", err)
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verify data artifact: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write verify data artifact: %w", err)
	}
	return nil
}

func loadVerifyDataArtifact(stateDir string) (verifyDataArtifact, error) {
	path := verifyDataArtifactPath(stateDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return verifyDataArtifact{}, fmt.Errorf("read verify data artifact: %w", err)
	}
	var artifact verifyDataArtifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return verifyDataArtifact{}, fmt.Errorf("parse verify data artifact: %w", err)
	}
	return artifact, nil
}
