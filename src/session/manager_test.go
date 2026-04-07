package session

import (
	"os"
	"path/filepath"
	"testing"
)

type mockTmux struct {
	nextWindowID   string
	windows        map[string]bool
	options        map[string]string
	lastNewCommand string
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		nextWindowID: "@1",
		windows:      make(map[string]bool),
		options:      make(map[string]string),
	}
}

func (m *mockTmux) NewWindow(name, command, startDir string) (string, error) {
	id := m.nextWindowID
	m.windows[id] = true
	m.lastNewCommand = command
	return id, nil
}

func (m *mockTmux) KillWindow(windowID string) error {
	delete(m.windows, windowID)
	return nil
}

func (m *mockTmux) ListWindowIDs() ([]string, error) {
	var ids []string
	for id := range m.windows {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockTmux) SetOption(target, key, value string) error {
	m.options[target+":"+key] = value
	return nil
}


func setupManager(t *testing.T) (*Manager, *mockTmux) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	tmux := newMockTmux()
	mgr := NewManager(tmux, dir)
	return mgr, tmux
}

func TestCreateAndAll(t *testing.T) {
	mgr, _ := setupManager(t)

	sess, err := mgr.Create("/tmp/proj", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if sess.WindowID != "@1" {
		t.Fatalf("expected @1, got %s", sess.WindowID)
	}
	if sess.State != StateRunning {
		t.Fatalf("expected Running, got %s", sess.State)
	}

	all := mgr.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session, got %d", len(all))
	}
	if all[0].ID != sess.ID {
		t.Fatal("session ID mismatch")
	}
}

func TestCreatePersistsToFile(t *testing.T) {
	mgr, tmux := setupManager(t)

	mgr.Create("/tmp/proj", "claude")

	mgr2 := NewManager(tmux, mgr.DataDir())
	mgr2.Refresh()

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session after reload, got %d", len(all))
	}
}

func TestStop(t *testing.T) {
	mgr, tmux := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")
	if err := mgr.Stop(sess.ID); err != nil {
		t.Fatal(err)
	}

	if len(mgr.All()) != 0 {
		t.Fatal("expected 0 sessions after stop")
	}
	if tmux.windows["@1"] {
		t.Fatal("expected window to be killed")
	}
}

func TestRefreshReconciles(t *testing.T) {
	mgr, tmux := setupManager(t)

	mgr.Create("/tmp/proj", "claude")
	if len(mgr.All()) != 1 {
		t.Fatal("expected 1 session")
	}

	// Simulate window killed externally
	delete(tmux.windows, "@1")

	mgr.Refresh()
	if len(mgr.All()) != 0 {
		t.Fatal("expected 0 sessions after reconcile")
	}
}

func TestUpdateStates(t *testing.T) {
	mgr, _ := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.UpdateStates(map[string]State{
		sess.WindowID: StateWaiting,
	})

	all := mgr.All()
	if all[0].State != StateWaiting {
		t.Fatalf("expected Waiting, got %s", all[0].State)
	}
}

func TestClear(t *testing.T) {
	mgr, _ := setupManager(t)

	mgr.Create("/tmp/proj", "claude")
	mgr.Clear()

	if len(mgr.All()) != 0 {
		t.Fatal("expected 0 sessions after clear")
	}
}

func TestFindByID(t *testing.T) {
	mgr, _ := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")

	found := mgr.FindByID(sess.ID)
	if found == nil {
		t.Fatal("expected to find session")
	}
	if found.ID != sess.ID {
		t.Fatal("ID mismatch")
	}

	if mgr.FindByID("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent ID")
	}
}

func TestCreateUsesExecPrefix(t *testing.T) {
	mgr, tmux := setupManager(t)

	mgr.Create("/tmp/proj", "claude")

	if tmux.lastNewCommand != "exec claude" {
		t.Fatalf("expected 'exec claude', got %q", tmux.lastNewCommand)
	}
}

func TestRefreshBranch(t *testing.T) {
	mgr, _ := setupManager(t)
	mgr.detectBranch = func(string) string { return "main" }

	sess, _ := mgr.Create("/tmp/proj", "claude")
	if len(sess.Tags) != 1 || sess.Tags[0].Text != "main" {
		t.Fatalf("expected tag main, got %v", sess.Tags)
	}

	// Simulate branch change
	mgr.detectBranch = func(string) string { return "feature" }
	if !mgr.RefreshBranch(sess.ID) {
		t.Fatal("expected true on branch change")
	}
	found := mgr.FindByID(sess.ID)
	if len(found.Tags) != 1 || found.Tags[0].Text != "feature" {
		t.Fatalf("expected tag feature, got %v", found.Tags)
	}

	// No change
	if mgr.RefreshBranch(sess.ID) {
		t.Fatal("expected false when unchanged")
	}

	// Non-existent ID
	if mgr.RefreshBranch("nonexistent") {
		t.Fatal("expected false for nonexistent ID")
	}

	// Verify persistence
	mgr2 := NewManager(newMockTmux(), mgr.DataDir())
	mgr2.load()
	found = mgr2.FindByID(sess.ID)
	if found == nil || len(found.Tags) != 1 || found.Tags[0].Text != "feature" {
		t.Fatalf("expected persisted tag feature, got %v", found.Tags)
	}
}

func TestByProject(t *testing.T) {
	mgr, tmux := setupManager(t)

	mgr.Create("/tmp/proj-a", "claude")
	tmux.nextWindowID = "@2"
	mgr.Create("/tmp/proj-b", "claude")
	tmux.nextWindowID = "@3"
	mgr.Create("/tmp/proj-a", "gemini")

	grouped := mgr.ByProject()
	if len(grouped["proj-a"]) != 2 {
		t.Fatalf("expected 2 sessions for proj-a, got %d", len(grouped["proj-a"]))
	}
	if len(grouped["proj-b"]) != 1 {
		t.Fatalf("expected 1 session for proj-b, got %d", len(grouped["proj-b"]))
	}
}

func TestFindByWindowID(t *testing.T) {
	mgr, _ := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	found := mgr.FindByWindowID(sess.WindowID)
	if found == nil || found.ID != sess.ID {
		t.Fatal("expected to find session by WindowID")
	}
	if mgr.FindByWindowID("@99") != nil {
		t.Fatal("expected nil for unknown WindowID")
	}
}

