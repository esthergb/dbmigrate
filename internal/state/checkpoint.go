package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const checkpointVersion = 1

// TableCheckpoint stores migration progress for one table.
type TableCheckpoint struct {
	RowsCopied int64     `json:"rows_copied"`
	KeyColumns []string  `json:"key_columns,omitempty"`
	LastKey    []string  `json:"last_key,omitempty"`
	Done       bool      `json:"done"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// DataCheckpoint stores migration progress for all copied tables.
type DataCheckpoint struct {
	Version             int                        `json:"version"`
	SourceWatermarkFile string                     `json:"source_watermark_file,omitempty"`
	SourceWatermarkPos  uint32                     `json:"source_watermark_pos,omitempty"`
	Tables              map[string]TableCheckpoint `json:"tables"`
}

// NewDataCheckpoint creates an empty checkpoint value.
func NewDataCheckpoint() DataCheckpoint {
	return DataCheckpoint{
		Version: checkpointVersion,
		Tables:  map[string]TableCheckpoint{},
	}
}

// LoadDataCheckpoint loads a checkpoint file or returns an empty checkpoint if the file is missing.
func LoadDataCheckpoint(path string) (DataCheckpoint, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewDataCheckpoint(), nil
		}
		return DataCheckpoint{}, fmt.Errorf("read checkpoint: %w", err)
	}

	var cp DataCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return DataCheckpoint{}, fmt.Errorf("parse checkpoint: %w", err)
	}
	if cp.Tables == nil {
		cp.Tables = map[string]TableCheckpoint{}
	}
	if cp.Version == 0 {
		cp.Version = checkpointVersion
	}
	return cp, nil
}

// SaveDataCheckpoint writes the checkpoint atomically.
func SaveDataCheckpoint(path string, checkpoint DataCheckpoint) error {
	if checkpoint.Version == 0 {
		checkpoint.Version = checkpointVersion
	}
	if checkpoint.Tables == nil {
		checkpoint.Tables = map[string]TableCheckpoint{}
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir checkpoint dir: %w", err)
	}

	raw, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write checkpoint temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace checkpoint: %w", err)
	}

	return nil
}
