package state

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"time"
)

const (
	checkpointKeyTypeNull   = "null"
	checkpointKeyTypeBytes  = "bytes"
	checkpointKeyTypeTime   = "time"
	checkpointKeyTypeBool   = "bool"
	checkpointKeyTypeInt64  = "int64"
	checkpointKeyTypeUint64 = "uint64"
	checkpointKeyTypeFloat  = "float64"
	checkpointKeyTypeString = "string"
)

// CheckpointKeyValue stores typed cursor values for resume-safe checkpointing.
type CheckpointKeyValue struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
}

// CursorValues decodes typed checkpoint cursor values.
func (t TableCheckpoint) CursorValues() ([]any, error) {
	if len(t.LastKeyTyped) > 0 {
		out := make([]any, 0, len(t.LastKeyTyped))
		for _, item := range t.LastKeyTyped {
			value, err := decodeCheckpointKeyValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	}
	if len(t.LastKey) == 0 {
		return nil, nil
	}
	// Legacy compatibility for existing checkpoints written with string-only cursors.
	out := make([]any, 0, len(t.LastKey))
	for _, value := range t.LastKey {
		out = append(out, value)
	}
	return out, nil
}

// SetCursorValues encodes typed cursor values for checkpoint persistence.
func (t *TableCheckpoint) SetCursorValues(values []any) error {
	if len(values) == 0 {
		t.LastKey = nil
		t.LastKeyTyped = nil
		return nil
	}
	out := make([]CheckpointKeyValue, 0, len(values))
	for _, value := range values {
		encoded, err := encodeCheckpointKeyValue(value)
		if err != nil {
			return err
		}
		out = append(out, encoded)
	}
	// Clear legacy field for new checkpoints; loader still supports old format.
	t.LastKey = nil
	t.LastKeyTyped = out
	return nil
}

func legacyCheckpointStrings(values []string) []CheckpointKeyValue {
	out := make([]CheckpointKeyValue, 0, len(values))
	for _, value := range values {
		out = append(out, CheckpointKeyValue{
			Type:  checkpointKeyTypeString,
			Value: value,
		})
	}
	return out
}

func encodeCheckpointKeyValue(value any) (CheckpointKeyValue, error) {
	switch typed := value.(type) {
	case nil:
		return CheckpointKeyValue{Type: checkpointKeyTypeNull}, nil
	case []byte:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeBytes,
			Value: base64.StdEncoding.EncodeToString(typed),
		}, nil
	case time.Time:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeTime,
			Value: typed.UTC().Format(time.RFC3339Nano),
		}, nil
	case bool:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeBool,
			Value: strconv.FormatBool(typed),
		}, nil
	case int:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeInt64,
			Value: strconv.FormatInt(int64(typed), 10),
		}, nil
	case int8:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeInt64,
			Value: strconv.FormatInt(int64(typed), 10),
		}, nil
	case int16:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeInt64,
			Value: strconv.FormatInt(int64(typed), 10),
		}, nil
	case int32:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeInt64,
			Value: strconv.FormatInt(int64(typed), 10),
		}, nil
	case int64:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeInt64,
			Value: strconv.FormatInt(typed, 10),
		}, nil
	case uint:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeUint64,
			Value: strconv.FormatUint(uint64(typed), 10),
		}, nil
	case uint8:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeUint64,
			Value: strconv.FormatUint(uint64(typed), 10),
		}, nil
	case uint16:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeUint64,
			Value: strconv.FormatUint(uint64(typed), 10),
		}, nil
	case uint32:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeUint64,
			Value: strconv.FormatUint(uint64(typed), 10),
		}, nil
	case uint64:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeUint64,
			Value: strconv.FormatUint(typed, 10),
		}, nil
	case float32:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeFloat,
			Value: strconv.FormatFloat(float64(typed), 'g', -1, 32),
		}, nil
	case float64:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeFloat,
			Value: strconv.FormatFloat(typed, 'g', -1, 64),
		}, nil
	case string:
		return CheckpointKeyValue{
			Type:  checkpointKeyTypeString,
			Value: typed,
		}, nil
	default:
		return CheckpointKeyValue{}, fmt.Errorf("unsupported checkpoint key value type %T", value)
	}
}

func decodeCheckpointKeyValue(value CheckpointKeyValue) (any, error) {
	switch value.Type {
	case checkpointKeyTypeNull:
		return nil, nil
	case checkpointKeyTypeBytes:
		decoded, err := base64.StdEncoding.DecodeString(value.Value)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key bytes: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeTime:
		decoded, err := time.Parse(time.RFC3339Nano, value.Value)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key time: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeBool:
		decoded, err := strconv.ParseBool(value.Value)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key bool: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeInt64:
		decoded, err := strconv.ParseInt(value.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key int64: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeUint64:
		decoded, err := strconv.ParseUint(value.Value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key uint64: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeFloat:
		decoded, err := strconv.ParseFloat(value.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("decode checkpoint key float64: %w", err)
		}
		return decoded, nil
	case checkpointKeyTypeString:
		return value.Value, nil
	default:
		return nil, fmt.Errorf("unsupported checkpoint key type %q", value.Type)
	}
}
