package runtime

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/state"
)

// newTestRelayAttached builds a Runtime with a standalone FileRelay
// (no background goroutines) attached for inspecting registration.
func newTestRelayAttached(t *testing.T) (*Runtime, *FileRelay) {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })

	fr := &FileRelay{
		watcher: w,
		files:   map[string]*relayFile{},
	}
	r := New(Config{
		SessionName: "roost-test",
		RoostExe:    "/usr/bin/roost",
		Tmux:        newFakeTmux(),
	})
	r.relay = fr
	return r, fr
}

// TestSyncRelayWatchesRegistersNewSessionLogTabs verifies that
// syncRelayWatches registers all LogTab paths from a newly-injected
// session into the FileRelay. This is the core fix for the bug where
// sessions created at runtime had their log tabs excluded from push updates.
func TestSyncRelayWatchesRegistersNewSessionLogTabs(t *testing.T) {
	dir := t.TempDir()
	transcriptPath := filepath.Join(dir, "transcript.jsonl")

	r, fr := newTestRelayAttached(t)

	// Inject a session directly into runtime state — mimics a session that
	// was created after SetRelay ran (internalSetRelay would miss it).
	sessID := state.SessionID("sess-1")
	r.state.Sessions = map[state.SessionID]state.Session{
		sessID: {
			ID:        sessID,
			Command:   "codex",
			CreatedAt: time.Now(),
			Driver: driver.CodexState{
				CommonState: driver.CommonState{
					TranscriptPath: transcriptPath,
				},
			},
		},
	}

	r.syncRelayWatches()

	if _, ok := fr.files[transcriptPath]; !ok {
		t.Errorf("syncRelayWatches did not register transcript path %s", transcriptPath)
	}
}

// TestSyncRelayWatchesNoRelayIsNoop verifies that syncRelayWatches is
// safe to call when no FileRelay has been attached.
func TestSyncRelayWatchesNoRelayIsNoop(t *testing.T) {
	r := New(Config{
		SessionName: "roost-test",
		RoostExe:    "/usr/bin/roost",
		Tmux:        newFakeTmux(),
	})
	// r.relay == nil; must not panic
	r.syncRelayWatches()
}
