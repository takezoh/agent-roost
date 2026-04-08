package session

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/take/agent-roost/session/driver"
)

type mockTmux struct {
	// nextWindowID, when non-empty, overrides the auto-increment counter for
	// the next NewWindow call. Tests can set it before Create to assert on
	// specific window IDs.
	nextWindowID   string
	windowCounter  int
	paneCounter    int
	windows        map[string]bool
	windowPanes    map[string]string // windowID → pane_id at pane index 0
	options        map[string]string // "windowID:key" → value
	userOptions    map[string]map[string]string
	lastNewCommand string
	commands       map[string]string // windowID → spawn command
	startDirs      map[string]string // windowID → startDir passed to NewWindow

	// failOptions, when non-nil, makes SetWindowUserOptions return this
	// error so tests can exercise the I/O failure path.
	failOptions error
}

func newMockTmux() *mockTmux {
	return &mockTmux{
		nextWindowID: "@1",
		windows:      make(map[string]bool),
		windowPanes:  make(map[string]string),
		options:      make(map[string]string),
		userOptions:  make(map[string]map[string]string),
		commands:     make(map[string]string),
		startDirs:    make(map[string]string),
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
	m.paneCounter++
	m.windowPanes[id] = fmt.Sprintf("%%%d", m.paneCounter)
	m.lastNewCommand = command
	m.commands[id] = command
	m.startDirs[id] = startDir
	return id, nil
}

func (m *mockTmux) KillWindow(windowID string) error {
	delete(m.windows, windowID)
	delete(m.userOptions, windowID)
	delete(m.windowPanes, windowID)
	return nil
}

func (m *mockTmux) DisplayMessage(target, format string) (string, error) {
	// Mirror real tmux: only the <window-id>.<pane-index> form resolves to
	// a pane. The legacy "@N:0.0" form silently returns empty (no error),
	// which is exactly how real tmux behaves and how the queryAgentPaneID
	// silent-failure bug went undetected before.
	if format != "#{pane_id}" {
		return "", nil
	}
	dot := strings.Index(target, ".")
	if dot < 0 {
		return "", nil
	}
	wid := target[:dot]
	if pid, ok := m.windowPanes[wid]; ok {
		return pid, nil
	}
	return "", nil
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
	if m.failOptions != nil {
		return m.failOptions
	}
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
			AgentPaneID:    opts["@roost_agent_pane"],
			DriverState:    opts["@roost_driver_state"],
			State:          opts["@roost_state"],
			StateChangedAt: opts["@roost_state_changed_at"],
		})
	}
	return out, nil
}

func setupManager(t *testing.T) (*Manager, *mockTmux) {
	t.Helper()
	tmux := newMockTmux()
	mgr := NewManager(tmux, t.TempDir(), driver.DefaultRegistry())
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

	mgr2 := NewManager(tmux, mgr.DataDir(), driver.DefaultRegistry())
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

func TestUpdateStates_PersistsToTmuxAndSnapshot(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	// Create writes the initial state proactively so warm restart finds
	// something even before the first poll cycle.
	if got := tmux.userOptions[sess.WindowID]["@roost_state"]; got != "running" {
		t.Fatalf("@roost_state after Create = %q, want running", got)
	}
	if tmux.userOptions[sess.WindowID]["@roost_state_changed_at"] == "" {
		t.Fatal("expected @roost_state_changed_at after Create")
	}

	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})

	if got := tmux.userOptions[sess.WindowID]["@roost_state"]; got != "waiting" {
		t.Fatalf("@roost_state after UpdateStates = %q, want waiting", got)
	}

	// Snapshot must carry the state for cold-boot Recreate.
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].State != StateWaiting {
		t.Fatalf("snapshot State = %+v, want waiting", loaded)
	}
	if loaded[0].StateChangedAt.IsZero() {
		t.Fatal("snapshot StateChangedAt should be set")
	}
}

func TestUpdateStates_NoOpDoesNotWriteTmux(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})
	before := tmux.userOptions[sess.WindowID]["@roost_state_changed_at"]

	// Identical state — must NOT bump state_changed_at or rewrite tmux.
	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})
	after := tmux.userOptions[sess.WindowID]["@roost_state_changed_at"]
	if before != after {
		t.Fatalf("state_changed_at moved on no-op update: %q -> %q", before, after)
	}
}

// Regression: UpdateStates must follow the "I/O 先行・状態変更後行" rule.
// If tmux write fails, the in-memory cache must remain unchanged so a later
// Refresh() doesn't see a state Manager believes is set but tmux never
// recorded.
func TestUpdateStates_TmuxFailureLeavesCacheUntouched(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	// Baseline: in-memory + tmux both hold StateRunning from Create.
	if got := mgr.FindByID(sess.ID).State; got != StateRunning {
		t.Fatalf("initial State = %s, want running", got)
	}

	tmux.failOptions = errors.New("tmux: simulated failure")
	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})

	if got := mgr.FindByID(sess.ID).State; got != StateRunning {
		t.Fatalf("State leaked through failed write: got %s, want running", got)
	}
	if got := tmux.userOptions[sess.WindowID]["@roost_state"]; got != "running" {
		t.Fatalf("@roost_state was clobbered by failed write: got %q", got)
	}

	// Recovery: clear the failure and try again. State should land cleanly.
	tmux.failOptions = nil
	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})
	if got := mgr.FindByID(sess.ID).State; got != StateWaiting {
		t.Fatalf("State after recovery = %s, want waiting", got)
	}
}

func TestRefresh_RestoresState(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	mgr.UpdateStates(map[string]State{sess.WindowID: StateWaiting})

	// Fresh Manager talking to the same tmux mock — simulates a Coordinator
	// warm restart.
	mgr2 := NewManager(tmux, mgr.DataDir(), driver.DefaultRegistry())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}
	restored := mgr2.FindByID(sess.ID)
	if restored == nil || restored.State != StateWaiting {
		t.Fatalf("expected restored State=waiting, got %+v", restored)
	}
	if restored.StateChangedAt.IsZero() {
		t.Fatal("expected restored StateChangedAt")
	}
}

func TestRecreate_ResetsStateForFreshSpawn(t *testing.T) {
	mgr1, _ := setupManager(t)
	sess, _ := mgr1.Create("/tmp/proj", "claude")
	// Prior Coordinator left the session in StateWaiting.
	mgr1.UpdateStates(map[string]State{sess.WindowID: StateWaiting})

	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, mgr1.DataDir(), driver.DefaultRegistry())
	if err := mgr2.Recreate(); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 recreated session, got %d", len(all))
	}
	// Cold boot spawned a fresh agent — state must reset to Running so the
	// TUI doesn't display a stale "waiting" left over from the prior process.
	if all[0].State != StateRunning {
		t.Errorf("Recreate State = %s, want running", all[0].State)
	}
	if all[0].StateChangedAt.IsZero() {
		t.Error("Recreate must set a fresh StateChangedAt")
	}
	if got := tmux2.userOptions[all[0].WindowID]["@roost_state"]; got != "running" {
		t.Errorf("@roost_state after Recreate = %q, want running", got)
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

func TestMergeDriverState_BasicSet(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	if !mgr.MergeDriverState(sess.WindowID, map[string]string{"session_id": "agent-1"}) {
		t.Fatal("expected true on first set")
	}
	if mgr.MergeDriverState(sess.WindowID, map[string]string{"session_id": "agent-1"}) {
		t.Fatal("expected false on no-op set")
	}
	if mgr.MergeDriverState("@nonexistent", map[string]string{"session_id": "x"}) {
		t.Fatal("expected false for unknown window")
	}

	if got := tmux.userOptions[sess.WindowID]["@roost_driver_state"]; got != `{"session_id":"agent-1"}` {
		t.Fatalf("@roost_driver_state = %q, want JSON for {session_id:agent-1}", got)
	}

	found := mgr.FindByID(sess.ID)
	if found.DriverState["session_id"] != "agent-1" {
		t.Fatalf("expected cache update, got %v", found.DriverState)
	}

	// A fresh Manager picks up the bag from tmux.
	mgr2 := NewManager(tmux, mgr.DataDir(), driver.DefaultRegistry())
	if err := mgr2.Refresh(); err != nil {
		t.Fatal(err)
	}
	restored := mgr2.FindByID(sess.ID)
	if restored == nil || restored.DriverState["session_id"] != "agent-1" {
		t.Fatalf("expected restored DriverState[session_id], got %+v", restored)
	}
}

func TestMergeDriverState_MultipleKeys(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	updates := map[string]string{
		"session_id":      "agent-1",
		"working_dir":     "/tmp/proj/.claude/worktrees/foo",
		"transcript_path": "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-1.jsonl",
	}
	if !mgr.MergeDriverState(sess.WindowID, updates) {
		t.Fatal("expected true on first set")
	}

	found := mgr.FindByID(sess.ID)
	for k, v := range updates {
		if found.DriverState[k] != v {
			t.Errorf("DriverState[%q] = %q, want %q", k, found.DriverState[k], v)
		}
	}

	encoded := tmux.userOptions[sess.WindowID]["@roost_driver_state"]
	if encoded == "" {
		t.Fatal("expected @roost_driver_state to be set")
	}
	if decoded := decodeDriverState(encoded); len(decoded) != 3 || decoded["session_id"] != "agent-1" {
		t.Fatalf("decoded driver state = %v", decoded)
	}

	// Snapshot carries the bag for cold-boot Recreate.
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].DriverState["transcript_path"] != updates["transcript_path"] {
		t.Fatalf("snapshot DriverState = %+v, want transcript_path %q", loaded, updates["transcript_path"])
	}
}

func TestMergeDriverState_DeleteOnEmpty(t *testing.T) {
	mgr, _ := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.MergeDriverState(sess.WindowID, map[string]string{"session_id": "agent-1", "tmp": "x"})
	if !mgr.MergeDriverState(sess.WindowID, map[string]string{"tmp": ""}) {
		t.Fatal("expected true when deleting an existing key")
	}
	found := mgr.FindByID(sess.ID)
	if _, ok := found.DriverState["tmp"]; ok {
		t.Errorf("expected tmp to be deleted, got %v", found.DriverState)
	}
	if found.DriverState["session_id"] != "agent-1" {
		t.Errorf("expected session_id to survive delete, got %v", found.DriverState)
	}
}

func TestMergeDriverState_RefreshesBranch(t *testing.T) {
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

	if !mgr.MergeDriverState(sess.WindowID, map[string]string{"working_dir": "/tmp/proj/.claude/worktrees/foo"}) {
		t.Fatal("expected MergeDriverState to report change")
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
	workdir := "/tmp/proj/.claude/worktrees/foo"
	tpath := "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-x.jsonl"
	mgr1.MergeDriverState(sess.WindowID, map[string]string{
		"session_id":      "agent-x",
		"working_dir":     workdir,
		"transcript_path": tpath,
	})

	dataDir := mgr1.DataDir()
	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, dataDir, driver.DefaultRegistry())
	if err := mgr2.Recreate(); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 recreated session, got %d", len(all))
	}
	if all[0].DriverState["working_dir"] != workdir {
		t.Errorf("working_dir not preserved: got %q, want %q", all[0].DriverState["working_dir"], workdir)
	}
	if all[0].DriverState["transcript_path"] != tpath {
		t.Errorf("transcript_path not preserved: got %q, want %q", all[0].DriverState["transcript_path"], tpath)
	}
	opts := tmux2.userOptions[all[0].WindowID]
	decoded := decodeDriverState(opts["@roost_driver_state"])
	if decoded["working_dir"] != workdir {
		t.Errorf("@roost_driver_state.working_dir not written on Recreate, got %q", decoded["working_dir"])
	}
	if decoded["transcript_path"] != tpath {
		t.Errorf("@roost_driver_state.transcript_path not written on Recreate, got %q", decoded["transcript_path"])
	}
}

// Cold-boot recovery for `claude --worktree` sessions: the new window must
// spawn inside the recorded worktree dir (not the original launch dir) and
// the spawn command must drop the --worktree flag, since Claude treats it
// as "create a new worktree" and is incompatible with --resume.
func TestRecreate_WorktreeUsesDriverWorkingDir(t *testing.T) {
	mgr1, _ := setupManager(t)
	sess, _ := mgr1.Create("/tmp/proj", "claude --worktree")
	worktree := "/tmp/proj/.claude/worktrees/foo"
	mgr1.MergeDriverState(sess.WindowID, map[string]string{
		"session_id":  "agent-x",
		"working_dir": worktree,
	})

	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, mgr1.DataDir(), driver.DefaultRegistry())
	if err := mgr2.Recreate(); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 recreated session, got %d", len(all))
	}
	wid := all[0].WindowID

	wantCmd := "exec claude --resume agent-x"
	if got := tmux2.commands[wid]; got != wantCmd {
		t.Errorf("spawn command = %q, want %q (--worktree must be stripped on resume)", got, wantCmd)
	}
	if got := tmux2.startDirs[wid]; got != worktree {
		t.Errorf("startDir = %q, want %q (must use Driver.WorkingDir for worktree sessions)", got, worktree)
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

func TestSnapshotUpdatedOnMergeDriverState(t *testing.T) {
	mgr, _ := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	mgr.MergeDriverState(sess.WindowID, map[string]string{"session_id": "agent-42"})

	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].DriverState["session_id"] != "agent-42" {
		t.Fatalf("expected snapshot DriverState[session_id]=agent-42, got %+v", loaded)
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
	mgr1.MergeDriverState(sessB.WindowID, map[string]string{"session_id": "agent-b"})

	dataDir := mgr1.DataDir()

	// Simulate PC reboot: brand new tmux mock with empty state, fresh
	// Manager pointing at the same dataDir.
	tmux2 := newMockTmux()
	mgr2 := NewManager(tmux2, dataDir, driver.DefaultRegistry())
	if err := mgr2.Recreate(); err != nil {
		t.Fatal(err)
	}

	all := mgr2.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 recreated sessions, got %d", len(all))
	}

	// Both windows should have been created in the new tmux instance with
	// the original IDs/projects/commands and DriverState preserved.
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
	if foundB.DriverState["session_id"] != "agent-b" {
		t.Errorf("expected DriverState[session_id]=agent-b, got %v", foundB.DriverState)
	}

	// tmux user options must be set on the new windows
	opts := tmux2.userOptions[foundB.WindowID]
	if opts == nil || opts["@roost_id"] != foundB.ID {
		t.Errorf("expected @roost_id on new window, got %v", opts)
	}
	if decoded := decodeDriverState(opts["@roost_driver_state"]); decoded["session_id"] != "agent-b" {
		t.Errorf("expected @roost_driver_state.session_id preserved, got %v", decoded)
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
	if err := mgr.Recreate(); err != nil {
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

// Regression: Create must persist @roost_agent_pane to tmux user options.
// Previously queryAgentPaneID built an invalid target ("@N:0.0") that real
// tmux silently resolved to nothing, leaving AgentPaneID empty and the
// reaper unable to find the session by pane id.
func TestCreate_PersistsAgentPaneIDUserOption(t *testing.T) {
	mgr, mt := setupManager(t)
	sess, err := mgr.Create("/tmp/proj", "claude")
	if err != nil {
		t.Fatal(err)
	}
	if sess.AgentPaneID == "" {
		t.Fatal("expected Session.AgentPaneID to be set")
	}
	got := mt.userOptions[sess.WindowID]["@roost_agent_pane"]
	if got == "" {
		t.Fatal("expected @roost_agent_pane user option to be set")
	}
	if got != sess.AgentPaneID {
		t.Fatalf("user option mismatch: got %q, want %q", got, sess.AgentPaneID)
	}
}

// Regression: Refresh must backfill AgentPaneID for sessions whose
// @roost_agent_pane user option is missing (legacy sessions created before
// pane id tracking landed). Without this, reapDeadPane00 cannot identify
// dead panes by pane id and silently bails out.
func TestRefresh_BackfillsMissingAgentPaneID(t *testing.T) {
	mgr, mt := setupManager(t)
	// Simulate a legacy session: window exists with all roost user options
	// EXCEPT @roost_agent_pane.
	mt.windows["@5"] = true
	mt.windowPanes["@5"] = "%42"
	mt.userOptions["@5"] = map[string]string{
		"@roost_id":         "legacy",
		"@roost_project":    "/tmp/proj",
		"@roost_command":    "claude",
		"@roost_created_at": "2026-04-08T16:00:00Z",
		"@roost_state":      "running",
	}

	if err := mgr.Refresh(); err != nil {
		t.Fatal(err)
	}

	sess := mgr.FindByWindowID("@5")
	if sess == nil {
		t.Fatal("expected legacy session to be loaded")
	}
	if sess.AgentPaneID != "%42" {
		t.Fatalf("expected backfilled AgentPaneID=%%42, got %q", sess.AgentPaneID)
	}
	if got := mt.userOptions["@5"]["@roost_agent_pane"]; got != "%42" {
		t.Fatalf("expected @roost_agent_pane user option to be backfilled, got %q", got)
	}
	// FindByAgentPaneID is what reapDeadPane00 uses; verify it now resolves.
	if found := mgr.FindByAgentPaneID("%42"); found == nil || found.ID != "legacy" {
		t.Fatal("expected FindByAgentPaneID to resolve backfilled session")
	}
}
