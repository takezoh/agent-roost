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
			ID:      "abc",
			Project: "/foo",
			Command: "claude",
			WindowID: "@5",
			AgentPaneID: "%10",
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

	// File exists
	if _, err := os.Stat(filepath.Join(dir, "sessions.json")); err != nil {
		t.Errorf("sessions.json not created: %v", err)
	}
	// And no .tmp left over
	if _, err := os.Stat(filepath.Join(dir, "sessions.json.tmp")); !os.IsNotExist(err) {
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
