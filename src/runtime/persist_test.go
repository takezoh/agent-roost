package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilePersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersist(dir)

	want := []SessionSnapshot{
		{
			ID:        "abc",
			Project:   "/foo",
			Command:   "claude",
			WindowID:  "@5",
			PaneID:    "%10",
			CreatedAt: "2026-04-10T12:00:00Z",
			Driver:    "claude",
			DriverState: map[string]string{
				"session_id": "uuid",
			},
		},
	}
	if err := p.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Per-session file exists
	if _, err := os.Stat(filepath.Join(dir, "sessions", "abc.json")); err != nil {
		t.Errorf("abc.json not created: %v", err)
	}
	// No .tmp left over
	if _, err := os.Stat(filepath.Join(dir, "sessions", "abc.json.tmp")); !os.IsNotExist(err) {
		t.Errorf("temp file leaked: %v", err)
	}

	got, err := p.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != "abc" || got[0].Driver != "claude" {
		t.Errorf("got = %+v", got[0])
	}
	if got[0].DriverState["session_id"] != "uuid" {
		t.Errorf("DriverState[session_id] = %q", got[0].DriverState["session_id"])
	}
}

func TestFilePersistLoadMissing(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersist(dir)
	got, err := p.Load()
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != nil {
		t.Errorf("got = %v, want nil", got)
	}
}

func TestFilePersistSaveEmpty(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersist(dir)
	if err := p.Save(nil); err != nil {
		t.Fatalf("Save nil: %v", err)
	}
	got, err := p.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFilePersistDeletesOrphanFiles(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersist(dir)

	// Save two sessions
	if err := p.Save([]SessionSnapshot{
		{ID: "s1", Project: "/a", Command: "claude"},
		{ID: "s2", Project: "/b", Command: "claude"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Remove s2
	if err := p.Save([]SessionSnapshot{
		{ID: "s1", Project: "/a", Command: "claude"},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// s2.json should be deleted
	if _, err := os.Stat(filepath.Join(dir, "sessions", "s2.json")); !os.IsNotExist(err) {
		t.Error("s2.json should have been deleted")
	}

	// s1.json should still exist
	if _, err := os.Stat(filepath.Join(dir, "sessions", "s1.json")); err != nil {
		t.Errorf("s1.json missing: %v", err)
	}
}

func TestFilePersistMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	p := NewFilePersist(dir)

	sessions := []SessionSnapshot{
		{ID: "aaa", Project: "/p1", Command: "claude"},
		{ID: "bbb", Project: "/p2", Command: "gemini"},
		{ID: "ccc", Project: "/p3", Command: "codex"},
	}
	if err := p.Save(sessions); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := p.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	ids := map[string]bool{}
	for _, s := range got {
		ids[s.ID] = true
	}
	for _, id := range []string{"aaa", "bbb", "ccc"} {
		if !ids[id] {
			t.Errorf("missing session %s", id)
		}
	}
}
