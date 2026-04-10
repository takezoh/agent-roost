package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FilePersist is the production PersistBackend. It writes the session
// snapshot atomically (write-temp + rename) to <dataDir>/sessions.json
// and reads it back via Load.
type FilePersist struct {
	path string
}

// NewFilePersist constructs a FilePersist anchored at the given data
// directory. The caller is expected to have already created the
// directory (cmd/main does this at startup).
func NewFilePersist(dataDir string) *FilePersist {
	return &FilePersist{path: filepath.Join(dataDir, "sessions.json")}
}

// Path returns the on-disk file path.
func (p *FilePersist) Path() string { return p.path }

// Save writes the snapshot to a temp file in the same directory and
// renames it over the target. The two-step write means a crash
// mid-save leaves the previous snapshot intact.
func (p *FilePersist) Save(sessions []SessionSnapshot) error {
	if sessions == nil {
		sessions = []SessionSnapshot{}
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("persist: marshal: %w", err)
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("persist: write temp: %w", err)
	}
	if err := os.Rename(tmp, p.path); err != nil {
		return fmt.Errorf("persist: rename: %w", err)
	}
	return nil
}

// Load reads the snapshot back. Returns (nil, nil) when the file does
// not exist (fresh install / no prior sessions).
func (p *FilePersist) Load() ([]SessionSnapshot, error) {
	data, err := os.ReadFile(p.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("persist: read: %w", err)
	}
	var out []SessionSnapshot
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("persist: unmarshal: %w", err)
	}
	return out, nil
}
