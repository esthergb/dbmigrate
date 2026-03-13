package state

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const serverIDFile = "replication-server-id.json"

type serverIDRecord struct {
	ServerID uint32 `json:"server_id"`
}

// LoadOrCreateServerID returns a stable replication server_id for this state-dir.
// If no server_id has been persisted yet, a random one in the MySQL-legal range
// [1, 4294967295] is generated and saved atomically.
// A non-zero explicitID overrides the persisted value and is returned as-is
// without modifying the file.
func LoadOrCreateServerID(stateDir string, explicitID uint32) (uint32, error) {
	if explicitID != 0 {
		return explicitID, nil
	}

	path := filepath.Join(stateDir, serverIDFile)

	raw, err := os.ReadFile(path)
	if err == nil {
		var rec serverIDRecord
		if jsonErr := json.Unmarshal(raw, &rec); jsonErr == nil && rec.ServerID != 0 {
			return rec.ServerID, nil
		}
	} else if !os.IsNotExist(err) {
		return 0, fmt.Errorf("read server_id file: %w", err)
	}

	newID, err := randomServerID()
	if err != nil {
		return 0, fmt.Errorf("generate server_id: %w", err)
	}

	rec := serverIDRecord{ServerID: newID}
	encoded, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal server_id: %w", err)
	}
	if err := WritePrivateFileAtomic(path, encoded); err != nil {
		return 0, fmt.Errorf("persist server_id: %w", err)
	}
	return newID, nil
}

func randomServerID() (uint32, error) {
	for {
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		id := binary.BigEndian.Uint32(buf[:])
		if id != 0 {
			return id, nil
		}
	}
}
