package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const checkpointVersion = 1

// TableCheckpoint stores migration progress for one table.
type TableCheckpoint struct {
	RowsCopied   int64                `json:"rows_copied"`
	KeyColumns   []string             `json:"key_columns,omitempty"`
	LastKey      []string             `json:"last_key,omitempty"` // legacy compatibility cursor storage
	LastKeyTyped []CheckpointKeyValue `json:"last_key_typed,omitempty"`
	Done         bool                 `json:"done"`
	UpdatedAt    time.Time            `json:"updated_at"`
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
	for tableKey, entry := range cp.Tables {
		if len(entry.LastKeyTyped) == 0 && len(entry.LastKey) > 0 {
			entry.LastKeyTyped = legacyCheckpointStrings(entry.LastKey)
			cp.Tables[tableKey] = entry
		}
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

	raw, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	return writePrivateFileAtomic(path, raw)
}
