package runtime

import (
	"testing"

	"github.com/fsnotify/fsnotify"

	"github.com/take/agent-roost/state"
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

	sid := state.SessionID("sess-1")
	fr.files["/tmp/a.log"] = &relayFile{path: "/tmp/a.log", sessionID: sid, kind: "transcript"}
	fr.files["/tmp/b.log"] = &relayFile{path: "/tmp/b.log", sessionID: sid, kind: "log"}
	fr.files["/tmp/c.log"] = &relayFile{path: "/tmp/c.log", sessionID: "other", kind: "log"}

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
	fr.files["/tmp/x.log"] = &relayFile{path: "/tmp/x.log", sessionID: "keep", kind: "log"}

	fr.UnwatchFile("nonexistent")

	if len(fr.files) != 1 {
		t.Errorf("expected 1 file remaining, got %d", len(fr.files))
	}
}

func TestUnwatch(t *testing.T) {
	fr := newTestFileRelay(t)
	fr.files["/tmp/d.log"] = &relayFile{path: "/tmp/d.log", sessionID: "s1", kind: "log"}
	fr.files["/tmp/e.log"] = &relayFile{path: "/tmp/e.log", sessionID: "s2", kind: "log"}

	fr.Unwatch("/tmp/d.log")

	if _, ok := fr.files["/tmp/d.log"]; ok {
		t.Error("expected /tmp/d.log to be removed")
	}
	if _, ok := fr.files["/tmp/e.log"]; !ok {
		t.Error("expected /tmp/e.log to remain")
	}
}
