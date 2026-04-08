package session

import (
	"fmt"
	"os"
	"testing"

	"github.com/take/agent-roost/session/driver"
)

type mockTmux struct {
	// nextWindowID, when non-empty, overrides the auto-increment counter for
	// the next NewWindow call. Tests can set it before Create to assert on
	// specific window IDs.
	nextWindowID   string
	windowCounter  int
	windows        map[string]bool
	options        map[string]string // "windowID:key" → value
	userOptions    map[string]map[string]string
	lastNewCommand string
	commands       map[string]string // windowID → spawn command
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		nextWindowID: "@1",
		windows:      make(map[string]bool),
		options:      make(map[string]string),
		userOptions:  make(map[string]map[string]string),
		commands:     make(map[string]string),
	}
}

func (m *mockTmux) NewWindow(name, command, startDir string) (string, error) {
	var id string
	if m.nextWindowID != "" {
		id = m.nextWindowID
		m.nextWindowID = ""
	} else {
		m.windowCounter++
		id = fmt.Sprintf("@%d", m.windowCounter+100)
	}
	m.windows[id] = true
	m.lastNewCommand = command
	m.commands[id] = command
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
			WindowID:            id,
			ID:                  opts["@roost_id"],
			Project:             opts["@roost_project"],
			Command:             opts["@roost_command"],
			CreatedAt:           opts["@roost_created_at"],
			Tags:                opts["@roost_tags"],
			AgentSessionID:      opts["@roost_agent_session"],
			AgentWorkingDir:     opts["@roost_agent_workdir"],
			AgentTranscriptPath: opts["@roost_agent_transcript"],
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

func TestSetAgentTranscriptPath(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	path := "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-1.jsonl"
	if !mgr.SetAgentTranscriptPath(sess.WindowID, path) {
		t.Fatal("expected true on first set")
	}
	if mgr.SetAgentTranscriptPath(sess.WindowID, path) {
		t.Fatal("expected false on no-op set")
	}
	if mgr.SetAgentTranscriptPath("@nonexistent", "x") {
		t.Fatal("expected false for unknown window")
	}

	if got := tmux.userOptions[sess.WindowID]["@roost_agent_transcript"]; got != path {
		t.Fatalf("tmux user option = %q, want %q", got, path)
	}

	found := mgr.FindByID(sess.ID)
	if found.AgentTranscriptPath != path {
		t.Fatalf("cache AgentTranscriptPath = %q, want %q", found.AgentTranscriptPath, path)
	}

	// A fresh Manager should pick up the persisted path from tmux.
	mgr2 := NewManager(tmux, mgr.DataDir())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}
	restored := mgr2.FindByID(sess.ID)
	if restored == nil || restored.AgentTranscriptPath != path {
		t.Fatalf("expected restored AgentTranscriptPath, got %+v", restored)
	}

	// Snapshot should also carry the value for cold-boot Recreate.
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].AgentTranscriptPath != path {
		t.Fatalf("snapshot AgentTranscriptPath = %+v, want %q", loaded, path)
	}
}

func TestSetAgentWorkingDir(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	workdir := "/tmp/proj/.claude/worktrees/foo"
	if !mgr.SetAgentWorkingDir(sess.WindowID, workdir) {
		t.Fatal("expected true on first set")
	}
	if mgr.SetAgentWorkingDir(sess.WindowID, workdir) {
		t.Fatal("expected false on no-op set")
	}
	if mgr.SetAgentWorkingDir("@nonexistent", "x") {
		t.Fatal("expected false for unknown window")
	}

	if got := tmux.userOptions[sess.WindowID]["@roost_agent_workdir"]; got != workdir {
		t.Fatalf("tmux user option = %q, want %q", got, workdir)
	}

	found := mgr.FindByID(sess.ID)
	if found.AgentWorkingDir != workdir {
		t.Fatalf("cache AgentWorkingDir = %q, want %q", found.AgentWorkingDir, workdir)
	}

	// Refresh from a fresh Manager to confirm persistence.
	mgr2 := NewManager(tmux, mgr.DataDir())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}
	if restored := mgr2.FindByID(sess.ID); restored == nil || restored.AgentWorkingDir != workdir {
		t.Fatalf("expected restored AgentWorkingDir, got %+v", restored)
	}
}

func TestSetAgentWorkingDir_RefreshesBranch(t *testing.T) {
	mgr, tmux := setupManager(t)
	mgr.detectBranch = func(p string) string {
		switch p {
		case "/tmp/proj":
			return "main"
		case "/tmp/proj/.claude/worktrees/foo":
			return "worktree-foo"
		}
		return ""
	}

	sess, _ := mgr.Create("/tmp/proj", "claude")
	if len(sess.Tags) != 1 || sess.Tags[0].Text != "main" {
		t.Fatalf("expected initial main tag, got %v", sess.Tags)
	}

	if !mgr.SetAgentWorkingDir(sess.WindowID, "/tmp/proj/.claude/worktrees/foo") {
		t.Fatal("expected SetAgentWorkingDir to report change")
	}
	updated := mgr.FindByID(sess.ID)
	if len(updated.Tags) != 1 || updated.Tags[0].Text != "worktree-foo" {
		t.Fatalf("expected branch tag to flip to worktree-foo, got %v", updated.Tags)
	}
	stored := tmux.userOptions[sess.WindowID]["@roost_tags"]
	if decoded := decodeTags(stored); len(decoded) != 1 || decoded[0].Text != "worktree-foo" {
		t.Fatalf("expected stored tag worktree-foo, got %v", decoded)
	}
}

func TestRecreate_PreservesAgentRuntime(t *testing.T) {
	mgr1, _ := setupManager(t)
	sess, _ := mgr1.Create("/tmp/proj", "claude")
	mgr1.SetAgentSessionID(sess.WindowID, "agent-x")
	workdir := "/tmp/proj/.claude/worktrees/foo"
	mgr1.SetAgentWorkingDir(sess.WindowID, workdir)
	tpath := "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-x.jsonl"
	mgr1.SetAgentTranscriptPath(sess.WindowID, tpath)

	dataDir := mgr1.DataDir()
	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, dataDir)
	if err := mgr2.Recreate(driver.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 recreated session, got %d", len(all))
	}
	if all[0].AgentWorkingDir != workdir {
		t.Errorf("AgentWorkingDir not preserved: got %q, want %q", all[0].AgentWorkingDir, workdir)
	}
	if all[0].AgentTranscriptPath != tpath {
		t.Errorf("AgentTranscriptPath not preserved: got %q, want %q", all[0].AgentTranscriptPath, tpath)
	}
	opts := tmux2.userOptions[all[0].WindowID]
	if opts["@roost_agent_workdir"] != workdir {
		t.Errorf("@roost_agent_workdir not written on Recreate, got %q", opts["@roost_agent_workdir"])
	}
	if opts["@roost_agent_transcript"] != tpath {
		t.Errorf("@roost_agent_transcript not written on Recreate, got %q", opts["@roost_agent_transcript"])
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

func TestSnapshotWrittenOnCreate(t *testing.T) {
	mgr, _ := setupManager(t)

	mgr.Create("/tmp/proj", "claude")

	if _, err := os.Stat(mgr.snapshotPath()); err != nil {
		t.Fatalf("expected snapshot file: %v", err)
	}
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 snapshot entry, got %d", len(loaded))
	}
	if loaded[0].Project != "/tmp/proj" || loaded[0].Command != "claude" {
		t.Fatalf("snapshot mismatch: %+v", loaded[0])
	}
}

func TestSnapshotUpdatedOnSetAgentSessionID(t *testing.T) {
	mgr, _ := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.SetAgentSessionID(sess.WindowID, "agent-42")

	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].AgentSessionID != "agent-42" {
		t.Fatalf("expected snapshot AgentSessionID=agent-42, got %+v", loaded)
	}
}

func TestSnapshotIsEmptyArrayAfterClear(t *testing.T) {
	mgr, _ := setupManager(t)
	mgr.Create("/tmp/proj", "claude")

	mgr.Clear()

	data, err := os.ReadFile(mgr.snapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "null" {
		t.Fatalf("snapshot must not be the literal 'null', got %q", string(data))
	}
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatalf("snapshot must be valid JSON after Clear: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty session list, got %d entries", len(loaded))
	}
}

func TestSnapshotRemovedOnStop(t *testing.T) {
	mgr, _ := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.Stop(sess.ID)

	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected empty snapshot after Stop, got %d entries", len(loaded))
	}
}

func TestRecreate(t *testing.T) {
	// First Manager creates two sessions, writing the snapshot.
	mgr1, tmux1 := setupManager(t)
	mgr1.Create("/tmp/proj-a", "claude")
	tmux1.nextWindowID = "@2"
	sessB, _ := mgr1.Create("/tmp/proj-b", "claude")
	mgr1.SetAgentSessionID(sessB.WindowID, "agent-b")

	dataDir := mgr1.DataDir()

	// Simulate PC reboot: brand new tmux mock with empty state, fresh
	// Manager pointing at the same dataDir.
	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, dataDir)
	if err := mgr2.Recreate(driver.DefaultRegistry()); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 recreated sessions, got %d", len(all))
	}

	// Both windows should have been created in the new tmux instance with
	// the original IDs/projects/commands and AgentSessionID preserved.
	var foundA, foundB *Session
	for _, s := range all {
		if s.Project == "/tmp/proj-a" {
			foundA = s
		}
		if s.Project == "/tmp/proj-b" {
			foundB = s
		}
		if s.Command != "claude" {
			t.Errorf("unexpected command: %q", s.Command)
		}
	}
	if foundA == nil || foundB == nil {
		t.Fatalf("expected both sessions recreated, got A=%v B=%v", foundA, foundB)
	}
	if foundB.AgentSessionID != "agent-b" {
		t.Errorf("expected AgentSessionID agent-b, got %q", foundB.AgentSessionID)
	}

	// tmux user options must be set on the new windows
	opts := tmux2.userOptions[foundB.WindowID]
	if opts == nil || opts["@roost_id"] != foundB.ID {
		t.Errorf("expected @roost_id on new window, got %v", opts)
	}
	if opts["@roost_agent_session"] != "agent-b" {
		t.Errorf("expected @roost_agent_session preserved, got %q", opts["@roost_agent_session"])
	}

	// Claude session B has an agent ID → spawn command must include --resume
	if got := tmux2.commands[foundB.WindowID]; got != "exec claude --resume agent-b" {
		t.Errorf("session B spawn command = %q, want %q", got, "exec claude --resume agent-b")
	}
	// Claude session A has no agent ID → plain claude
	if got := tmux2.commands[foundA.WindowID]; got != "exec claude" {
		t.Errorf("session A spawn command = %q, want %q", got, "exec claude")
	}
}

func TestRecreate_NoSnapshot(t *testing.T) {
	mgr, _ := setupManager(t)
	if err := mgr.Recreate(driver.DefaultRegistry()); err != nil {
		t.Fatalf("expected nil error on missing snapshot, got %v", err)
	}
	if len(mgr.All()) != 0 {
		t.Fatalf("expected no sessions, got %d", len(mgr.All()))
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
