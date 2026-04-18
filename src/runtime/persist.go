package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FilePersist is the production PersistBackend. It writes each session
// to an individual file under <dataDir>/sessions/<id>.json with atomic
// temp+rename. Sessions that are no longer present are deleted.
type FilePersist struct {
	dir          string
	lastKnownIDs map[string]struct{} // nil = first Save not yet called
}

// NewFilePersist constructs a FilePersist anchored at the given data
// directory. The sessions subdirectory is created lazily on first Save.
func NewFilePersist(dataDir string) *FilePersist {
	return &FilePersist{dir: filepath.Join(dataDir, "sessions")}
}

// Save writes each session to its own file and removes files for
// sessions that are no longer in the list. ReadDir is skipped when the
// set of session IDs has not shrunk since the last call, avoiding a
// redundant directory scan on every tick.
func (p *FilePersist) Save(sessions []SessionSnapshot) error {
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return fmt.Errorf("persist: mkdir: %w", err)
	}

	want := make(map[string]struct{}, len(sessions))
	for _, sess := range sessions {
		want[sess.ID] = struct{}{}
		if err := p.writeOne(sess); err != nil {
			return err
		}
	}

	needPrune := p.lastKnownIDs == nil // first call: always prune
	if !needPrune {
		for id := range p.lastKnownIDs {
			if _, ok := want[id]; !ok {
				needPrune = true
				break
			}
		}
	}

	if needPrune {
		if err := p.pruneObsolete(want); err != nil {
			return err
		}
	}

	p.lastKnownIDs = want
	return nil
}

func (p *FilePersist) pruneObsolete(want map[string]struct{}) error {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return fmt.Errorf("persist: readdir: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if _, ok := want[id]; !ok {
			os.Remove(filepath.Join(p.dir, name))
		}
	}
	return nil
}

func (p *FilePersist) writeOne(sess SessionSnapshot) error {
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return fmt.Errorf("persist: marshal %s: %w", sess.ID, err)
	}
	target := filepath.Join(p.dir, sess.ID+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("persist: write %s: %w", sess.ID, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("persist: rename %s: %w", sess.ID, err)
	}
	return nil
}

// Load reads all session files from the directory. Returns (nil, nil)
// when the directory does not exist (fresh install).
func (p *FilePersist) Load() ([]SessionSnapshot, error) {
	entries, err := os.ReadDir(p.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("persist: readdir: %w", err)
	}

	var out []SessionSnapshot
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.dir, name))
		if err != nil {
			return nil, fmt.Errorf("persist: read %s: %w", name, err)
		}
		var snap SessionSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			return nil, fmt.Errorf("persist: unmarshal %s: %w", name, err)
		}
		out = append(out, snap)
	}
	return out, nil
}
