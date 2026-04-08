package session

import (
	"testing"
)

type mockTmux struct {
	nextWindowID   string
	windows        map[string]bool
	options        map[string]string // "windowID:key" → value
	userOptions    map[string]map[string]string
	lastNewCommand string
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		nextWindowID: "@1",
		windows:      make(map[string]bool),
		options:      make(map[string]string),
		userOptions:  make(map[string]map[string]string),
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
	delete(m.userOptions, windowID)
	return nil
}

func (m *mockTmux) SetOption(target, key, value string) error {
	m.options[target+":"+key] = value
	return nil
}

func (m *mockTmux) SetWindowUserOption(windowID, key, value string) error {
	if _, ok := m.userOptions[windowID]; !ok {
		m.userOptions[windowID] = make(map[string]string)
	}
	m.userOptions[windowID][key] = value
	return nil
}

func (m *mockTmux) SetWindowUserOptions(windowID string, kv map[string]string) error {
	if _, ok := m.userOptions[windowID]; !ok {
		m.userOptions[windowID] = make(map[string]string)
	}
	for k, v := range kv {
		m.userOptions[windowID][k] = v
	}
	return nil
}

func (m *mockTmux) ListRoostWindows() ([]RoostWindow, error) {
	var out []RoostWindow
	for id := range m.windows {
		opts := m.userOptions[id]
		if opts == nil || opts["@roost_id"] == "" {
			continue
		}
		out = append(out, RoostWindow{
			WindowID:       id,
			ID:             opts["@roost_id"],
			Project:        opts["@roost_project"],
			Command:        opts["@roost_command"],
			CreatedAt:      opts["@roost_created_at"],
			Tags:           opts["@roost_tags"],
			AgentSessionID: opts["@roost_agent_session"],
		})
	}
	return out, nil
}

func setupManager(t *testing.T) (*Manager, *mockTmux) {
	t.Helper()
	tmux := newMockTmux()
	mgr := NewManager(tmux, t.TempDir())
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

func TestCreateWritesUserOptions(t *testing.T) {
	mgr, tmux := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")
	opts := tmux.userOptions[sess.WindowID]
	if opts == nil {
		t.Fatal("expected user options to be set")
	}
	if opts["@roost_id"] != sess.ID {
		t.Fatalf("@roost_id mismatch: %q vs %q", opts["@roost_id"], sess.ID)
	}
	if opts["@roost_project"] != "/tmp/proj" {
		t.Fatalf("@roost_project mismatch: %q", opts["@roost_project"])
	}
	if opts["@roost_command"] != "claude" {
		t.Fatalf("@roost_command mismatch: %q", opts["@roost_command"])
	}
	if opts["@roost_created_at"] == "" {
		t.Fatal("@roost_created_at should be set")
	}
}

func TestRefreshFromTmux(t *testing.T) {
	mgr, tmux := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")
	originalID := sess.ID
	originalWID := sess.WindowID

	mgr2 := NewManager(tmux, mgr.DataDir())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 session after refresh, got %d", len(all))
	}
	if all[0].ID != originalID || all[0].WindowID != originalWID {
		t.Fatalf("session not restored: %+v", all[0])
	}
	if all[0].Project != "/tmp/proj" || all[0].Command != "claude" {
		t.Fatalf("metadata not restored: %+v", all[0])
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
	if _, ok := tmux.userOptions["@1"]; ok {
		t.Fatal("expected user options to be cleared with the window")
	}
}

func TestRefreshSkipsNonRoostWindows(t *testing.T) {
	mgr, tmux := setupManager(t)

	// Window without @roost_id should be ignored
	tmux.windows["@99"] = true
	mgr.Create("/tmp/proj", "claude")

	if err := mgr.Refresh(); err != nil {
		t.Fatal(err)
	}
	if len(mgr.All()) != 1 {
		t.Fatalf("expected 1 roost-managed session, got %d", len(mgr.All()))
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
	mgr, tmux := setupManager(t)
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

	// Verify tags were written to tmux user option
	stored := tmux.userOptions[sess.WindowID]["@roost_tags"]
	if stored == "" {
		t.Fatal("expected @roost_tags to be set in tmux")
	}
	if decoded := decodeTags(stored); len(decoded) != 1 || decoded[0].Text != "feature" {
		t.Fatalf("expected stored tag feature, got %v", decoded)
	}
}

func TestSetAgentSessionID(t *testing.T) {
	mgr, tmux := setupManager(t)

	sess, _ := mgr.Create("/tmp/proj", "claude")

	if !mgr.SetAgentSessionID(sess.WindowID, "agent-1") {
		t.Fatal("expected true on first set")
	}
	if mgr.SetAgentSessionID(sess.WindowID, "agent-1") {
		t.Fatal("expected false on no-op set")
	}
	if mgr.SetAgentSessionID("@nonexistent", "agent-x") {
		t.Fatal("expected false for unknown window")
	}

	if tmux.userOptions[sess.WindowID]["@roost_agent_session"] != "agent-1" {
		t.Fatalf("expected tmux user option to be set, got %v", tmux.userOptions[sess.WindowID])
	}

	found := mgr.FindByID(sess.ID)
	if found.AgentSessionID != "agent-1" {
		t.Fatalf("expected cache update, got %q", found.AgentSessionID)
	}

	// Verify a fresh Manager picks up the binding from tmux
	mgr2 := NewManager(tmux, mgr.DataDir())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}
	found = mgr2.FindByID(sess.ID)
	if found == nil || found.AgentSessionID != "agent-1" {
		t.Fatalf("expected restored AgentSessionID, got %+v", found)
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
