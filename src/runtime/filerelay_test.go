package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/takezoh/agent-roost/state"
)

func newTestFileRelay(t *testing.T) *FileRelay {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return &FileRelay{
		watcher: w,
		files:   map[string]*relayFile{},
	}
}

func TestUnwatchFile(t *testing.T) {
	fr := newTestFileRelay(t)

	sid := state.FrameID("sess-1")
	fr.files["/tmp/a.log"] = &relayFile{path: "/tmp/a.log", frameID: sid, kind: "transcript"}
	fr.files["/tmp/b.log"] = &relayFile{path: "/tmp/b.log", frameID: sid, kind: "log"}
	fr.files["/tmp/c.log"] = &relayFile{path: "/tmp/c.log", frameID: "other", kind: "log"}

	fr.UnwatchFile(sid)

	if _, ok := fr.files["/tmp/a.log"]; ok {
		t.Error("expected /tmp/a.log to be removed")
	}
	if _, ok := fr.files["/tmp/b.log"]; ok {
		t.Error("expected /tmp/b.log to be removed")
	}
	if _, ok := fr.files["/tmp/c.log"]; !ok {
		t.Error("expected /tmp/c.log to remain")
	}
}

func TestUnwatchFileNoMatch(t *testing.T) {
	fr := newTestFileRelay(t)
	fr.files["/tmp/x.log"] = &relayFile{path: "/tmp/x.log", frameID: "keep", kind: "log"}

	fr.UnwatchFile("nonexistent")

	if len(fr.files) != 1 {
		t.Errorf("expected 1 file remaining, got %d", len(fr.files))
	}
}

func TestUnwatch(t *testing.T) {
	fr := newTestFileRelay(t)
	fr.files["/tmp/d.log"] = &relayFile{path: "/tmp/d.log", frameID: "s1", kind: "log"}
	fr.files["/tmp/e.log"] = &relayFile{path: "/tmp/e.log", frameID: "s2", kind: "log"}

	fr.Unwatch("/tmp/d.log")

	if _, ok := fr.files["/tmp/d.log"]; ok {
		t.Error("expected /tmp/d.log to be removed")
	}
	if _, ok := fr.files["/tmp/e.log"]; !ok {
		t.Error("expected /tmp/e.log to remain")
	}
}

func TestWatchFileCreatesMissingPath(t *testing.T) {
	dir := t.TempDir()
	fr := newTestFileRelay(t)

	// Register a path whose parent dir does not exist yet.
	// The touch inside add() will fail silently, but the entry is still recorded.
	missingDir := filepath.Join(dir, "events", "sess-1.log")
	fr.WatchFile("sess-1", missingDir, "text")
	if _, ok := fr.files[missingDir]; !ok {
		t.Fatalf("expected %s to be registered in fr.files", missingDir)
	}

	// With a pre-existing parent dir, touch creates the file and the fsnotify
	// watch succeeds.
	eventsDir := filepath.Join(dir, "events2")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(eventsDir, "sess-2.log")
	fr.WatchFile("sess-2", path, "text")
	if _, ok := fr.files[path]; !ok {
		t.Fatalf("expected %s to be registered in fr.files", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to be created by touch: %v", path, err)
	}
}

func TestWatchFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	fr := newTestFileRelay(t)

	path := filepath.Join(dir, "roost.log")
	if err := os.WriteFile(path, []byte("line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	fr.WatchFile("frame-1", path, "log")
	fr.WatchFile("frame-1", path, "log")

	if len(fr.files) != 1 {
		t.Errorf("fr.files len = %d, want 1 (idempotent)", len(fr.files))
	}
}
