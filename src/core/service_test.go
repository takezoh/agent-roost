package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

type mockPaneOp struct {
	chainCalls  [][]string
	selectCalls []string
	paneDead    map[string]bool // target → dead?
}

func (m *mockPaneOp) SwapPane(src, dst string) error                { return nil }
func (m *mockPaneOp) SelectPane(target string) error                { m.selectCalls = append(m.selectCalls, target); return nil }
func (m *mockPaneOp) RespawnPane(target, cmd string) error          { return nil }
func (m *mockPaneOp) RunChain(cmds ...[]string) error               { m.chainCalls = cmds; return nil }
func (m *mockPaneOp) WindowIDFromPane(paneID string) (string, error) { return "@0", nil }
func (m *mockPaneOp) DisplayMessage(target, format string) (string, error) {
	if format == "#{pane_dead}" {
		if m.paneDead[target] {
			return "1", nil
		}
		return "0", nil
	}
	return "", nil
}

type mockTmuxForService struct {
	nextID      string
	windows     map[string]bool
	userOptions map[string]map[string]string
}

func (m *mockTmuxForService) NewWindow(name, command, startDir string) (string, error) {
	id := m.nextID
	m.windows[id] = true
	return id, nil
}
func (m *mockTmuxForService) KillWindow(windowID string) error {
	delete(m.windows, windowID)
	delete(m.userOptions, windowID)
	return nil
}
func (m *mockTmuxForService) SetOption(target, key, value string) error { return nil }
func (m *mockTmuxForService) SetWindowUserOption(windowID, key, value string) error {
	if m.userOptions == nil {
		m.userOptions = make(map[string]map[string]string)
	}
	if m.userOptions[windowID] == nil {
		m.userOptions[windowID] = make(map[string]string)
	}
	m.userOptions[windowID][key] = value
	return nil
}
func (m *mockTmuxForService) SetWindowUserOptions(windowID string, kv map[string]string) error {
	for k, v := range kv {
		m.SetWindowUserOption(windowID, k, v)
	}
	return nil
}
func (m *mockTmuxForService) ListRoostWindows() ([]session.RoostWindow, error) {
	var out []session.RoostWindow
	for id := range m.windows {
		opts := m.userOptions[id]
		if opts == nil || opts["@roost_id"] == "" {
			continue
		}
		out = append(out, session.RoostWindow{
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

type mockCapturer struct{ content map[string]string }

func (m *mockCapturer) CapturePaneLines(target string, n int) (string, error) {
	return m.content[target], nil
}

func setupService(t *testing.T) (*Service, *mockPaneOp, *session.Manager) {
	t.Helper()
	svc, panes, mgr, _ := setupServiceWithTmux(t)
	return svc, panes, mgr
}

func setupServiceWithTmux(t *testing.T) (*Service, *mockPaneOp, *session.Manager, *mockTmuxForService) {
	t.Helper()
	mt := &mockTmuxForService{nextID: "@1", windows: make(map[string]bool)}
	mgr := session.NewManager(mt, t.TempDir())
	store := driver.NewAgentStore()
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, store, driver.DefaultRegistry(), mon, panes, "roost", "", "")
	return svc, panes, mgr, mt
}

func TestBuildSwapChain_NoActive(t *testing.T) {
	svc, panes, _ := setupService(t)
	sess := &session.Session{ID: "abc", WindowID: "@2"}
	if err := svc.Preview(sess); err != nil {
		t.Fatal(err)
	}
	if len(panes.chainCalls) != 1 {
		t.Fatalf("expected 1 command, got %d", len(panes.chainCalls))
	}
}

func TestBuildSwapChain_WithActive(t *testing.T) {
	svc, panes, _ := setupService(t)
	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	svc.Preview(&session.Session{ID: "b", WindowID: "@3"})
	if len(panes.chainCalls) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(panes.chainCalls))
	}
}

func TestSwitch(t *testing.T) {
	svc, panes, _ := setupService(t)
	sess := &session.Session{ID: "abc", WindowID: "@2"}
	if err := svc.Switch(sess); err != nil {
		t.Fatal(err)
	}
	if len(panes.selectCalls) != 1 || panes.selectCalls[0] != "roost:0.0" {
		t.Fatalf("expected SelectPane roost:0.0, got %v", panes.selectCalls)
	}
}

func TestRefreshSessions_Changed(t *testing.T) {
	mt := &mockTmuxForService{nextID: "@1", windows: make(map[string]bool)}
	dataDir := t.TempDir()
	mgr := session.NewManager(mt, dataDir)
	store := driver.NewAgentStore()
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, store, driver.DefaultRegistry(), mon, panes, "roost", "", "")

	// Create via a separate manager so svc.Manager has empty in-memory state.
	mgr2 := session.NewManager(mt, dataDir)
	mgr2.Create("/tmp/proj", "echo hi")

	changed, latest := svc.RefreshSessions()
	if !changed {
		t.Fatal("expected changed=true on first refresh")
	}
	if latest == nil {
		t.Fatal("expected latest session")
	}

	changed, _ = svc.RefreshSessions()
	if changed {
		t.Fatal("expected changed=false on second refresh")
	}
}

func TestDeactivate_NoActive(t *testing.T) {
	svc, panes, _ := setupService(t)
	if err := svc.Deactivate(); err != nil {
		t.Fatal(err)
	}
	if len(panes.chainCalls) != 0 {
		t.Fatalf("expected 0 commands, got %d", len(panes.chainCalls))
	}
}

func TestDeactivate_WithActive(t *testing.T) {
	svc, panes, _ := setupService(t)
	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	if err := svc.Deactivate(); err != nil {
		t.Fatal(err)
	}
	if len(panes.chainCalls) != 1 {
		t.Fatalf("expected 1 command, got %d", len(panes.chainCalls))
	}
	if svc.ActiveWindowID() != "" {
		t.Fatalf("expected empty, got %s", svc.ActiveWindowID())
	}
}

func TestActiveWindowID(t *testing.T) {
	svc, _, _ := setupService(t)
	if svc.ActiveWindowID() != "" {
		t.Fatal("expected empty activeWindowID initially")
	}
	svc.Preview(&session.Session{ID: "x", WindowID: "@5"})
	if svc.ActiveWindowID() != "@5" {
		t.Fatalf("expected @5, got %s", svc.ActiveWindowID())
	}
}

func TestClearActive_MatchingWindow(t *testing.T) {
	svc, panes, _ := setupService(t)
	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	svc.ClearActive("@2")
	if svc.ActiveWindowID() != "" {
		t.Fatalf("expected empty, got %s", svc.ActiveWindowID())
	}
	svc.Preview(&session.Session{ID: "b", WindowID: "@3"})
	if len(panes.chainCalls) != 1 {
		t.Fatalf("expected 1 command after clear, got %d", len(panes.chainCalls))
	}
}

func TestClearActive_NonMatchingWindow(t *testing.T) {
	svc, _, _ := setupService(t)
	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	svc.ClearActive("@99")
	if svc.ActiveWindowID() != "@2" {
		t.Fatalf("expected @2, got %s", svc.ActiveWindowID())
	}
}

func TestNewService_RestoresActiveWindowID(t *testing.T) {
	mt := &mockTmuxForService{nextID: "@1", windows: make(map[string]bool)}
	mgr := session.NewManager(mt, t.TempDir())
	store := driver.NewAgentStore()
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, store, driver.DefaultRegistry(), mon, panes, "roost", "", "@5")
	if svc.ActiveWindowID() != "@5" {
		t.Fatalf("expected @5, got %s", svc.ActiveWindowID())
	}
}

func TestSyncActiveCallback(t *testing.T) {
	svc, _, _ := setupService(t)
	var synced []string
	svc.SetSyncActive(func(wid string) { synced = append(synced, wid) })

	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	svc.Preview(&session.Session{ID: "b", WindowID: "@3"})
	svc.Deactivate()

	want := []string{"@2", "@3", ""}
	if len(synced) != len(want) {
		t.Fatalf("expected %d sync calls, got %d", len(want), len(synced))
	}
	for i, w := range want {
		if synced[i] != w {
			t.Fatalf("sync[%d]: expected %q, got %q", i, w, synced[i])
		}
	}
}

func TestPreviewEmitsOnPreview(t *testing.T) {
	svc, _, _ := setupService(t)
	var ids1, ids2 []string
	svc.OnPreview(func(id string) { ids1 = append(ids1, id) })
	svc.OnPreview(func(id string) { ids2 = append(ids2, id) })

	svc.Preview(&session.Session{ID: "a", WindowID: "@2"})
	svc.Preview(&session.Session{ID: "b", WindowID: "@3"})

	want := []string{"a", "b"}
	for _, ids := range [][]string{ids1, ids2} {
		if len(ids) != 2 {
			t.Fatalf("expected 2 calls, got %d", len(ids))
		}
		for i, w := range want {
			if ids[i] != w {
				t.Fatalf("ids[%d]: expected %q, got %q", i, w, ids[i])
			}
		}
	}
}

func TestHandleStateChangeWithContext_AutoBind(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)

	// No prior session-start: state-change with pane should auto-bind
	changed := svc.HandleStateChangeWithContext("new-agent", driver.AgentStateRunning, "%0")
	if !changed {
		t.Fatal("expected true on auto-bind state change")
	}
	agent := svc.AgentStore.GetByWindow(sess.WindowID)
	if agent == nil {
		t.Fatal("expected agent after auto-bind")
	}
	if agent.ID != "new-agent" {
		t.Errorf("got ID %q, want %q", agent.ID, "new-agent")
	}
	if agent.State != driver.AgentStateRunning {
		t.Errorf("got state %v, want running", agent.State)
	}
}

func TestHandleStateChangeWithContext_NoPane(t *testing.T) {
	svc, _, _ := setupService(t)

	// No pane: should not auto-bind, returns false
	changed := svc.HandleStateChangeWithContext("unknown", driver.AgentStateRunning, "")
	if changed {
		t.Fatal("expected false without pane")
	}
	if svc.AgentStore.Get("unknown") != nil {
		t.Fatal("should not create session without pane")
	}
}

func TestHandleStateChangeWithContext_KnownSession(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)

	// Bind normally first
	svc.HandleSessionStart("%0", "agent-1")

	// state-change on known session works without re-binding
	changed := svc.HandleStateChangeWithContext("agent-1", driver.AgentStateRunning, "%0")
	if !changed {
		t.Fatal("expected true on state change")
	}
	if svc.AgentStore.Get("agent-1").State != driver.AgentStateRunning {
		t.Fatal("state not updated")
	}
}

func TestHandleStatusLine(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	svc.HandleSessionStart("%0", "agent-1")

	changed := svc.HandleStatusLine("agent-1", "thinking...")
	if !changed {
		t.Fatal("expected true on status line update")
	}
	agent := svc.AgentStore.GetByWindow(sess.WindowID)
	if agent == nil || agent.ID != "agent-1" {
		t.Fatal("expected agent after bind")
	}
	if agent.StatusLine != "thinking..." {
		t.Errorf("got status %q, want %q", agent.StatusLine, "thinking...")
	}
}

func TestHandleSessionStart_PersistsToTmux(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)

	if !svc.HandleSessionStart("%0", "agent-9") {
		t.Fatal("expected true on first bind")
	}
	found := mgr.FindByWindowID(sess.WindowID)
	if found == nil || found.AgentSessionID != "agent-9" {
		t.Fatalf("expected manager cache to carry agent-9, got %+v", found)
	}
}

// Regression: when multiple Claude sessions live in the same project and the
// AgentStore has not been populated (e.g. after a Coordinator restart),
// ResolveAgentMeta must NOT auto-bind by reading the newest .jsonl, because
// the same file would get attached to every window.
func TestResolveAgentMeta_DoesNotAutoBind(t *testing.T) {
	svc, _, mgr, mt := setupServiceWithTmux(t)

	a, _ := mgr.Create("/tmp/proj", "claude")
	mt.nextID = "@2"
	b, _ := mgr.Create("/tmp/proj", "claude")

	if a.WindowID == b.WindowID {
		t.Fatal("expected distinct window IDs")
	}

	// Neither session has a hook-fired binding. ResolveAgentMeta must leave
	// them both unbound rather than guess from the newest jsonl in the project.
	svc.ResolveAgentMeta()

	if svc.AgentStore.GetByWindow(a.WindowID) != nil {
		t.Fatal("session a should remain unbound")
	}
	if svc.AgentStore.GetByWindow(b.WindowID) != nil {
		t.Fatal("session b should remain unbound")
	}
}

func TestHandleAgentTranscriptPath_PersistsToManagerAndTmux(t *testing.T) {
	svc, _, mgr, mt := setupServiceWithTmux(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess) // sets activeWindowID so HandleSessionStart can fall back
	svc.HandleSessionStart("%0", "agent-1")

	path := "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-1.jsonl"
	if !svc.HandleAgentTranscriptPath("agent-1", path) {
		t.Fatal("expected true on first transcript path set")
	}
	updated := mgr.FindByWindowID(sess.WindowID)
	if updated == nil || updated.AgentTranscriptPath != path {
		t.Fatalf("Session.AgentTranscriptPath not updated, got %+v", updated)
	}
	if got := mt.userOptions[sess.WindowID]["@roost_agent_transcript"]; got != path {
		t.Fatalf("@roost_agent_transcript = %q, want %q", got, path)
	}

	// Idempotent: same path is a no-op.
	if svc.HandleAgentTranscriptPath("agent-1", path) {
		t.Fatal("expected false on idempotent set")
	}
	// Empty inputs are no-ops too.
	if svc.HandleAgentTranscriptPath("", path) {
		t.Fatal("expected false on empty agent ID")
	}
	if svc.HandleAgentTranscriptPath("agent-1", "") {
		t.Fatal("expected false on empty path")
	}
}

func TestHandleAgentTranscriptPath_BeforeBindIsNoop(t *testing.T) {
	svc, _, _ := setupService(t)
	// Without a binding there is no window to attach the path to. The next
	// hook event will carry the path again so dropping it here is safe.
	if svc.HandleAgentTranscriptPath("agent-orphan", "/x/y/agent-orphan.jsonl") {
		t.Fatal("expected false without binding")
	}
}

func TestHandleAgentWorkingDir_PersistsAndRefreshesBranch(t *testing.T) {
	svc, _, mgr, mt := setupServiceWithTmux(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	svc.HandleSessionStart("%0", "agent-1")

	workdir := "/tmp/proj/.claude/worktrees/foo"
	if !svc.HandleAgentWorkingDir("agent-1", workdir) {
		t.Fatal("expected true on first set")
	}
	updated := mgr.FindByWindowID(sess.WindowID)
	if updated == nil || updated.AgentWorkingDir != workdir {
		t.Fatalf("Session.AgentWorkingDir not updated, got %+v", updated)
	}
	if got := mt.userOptions[sess.WindowID]["@roost_agent_workdir"]; got != workdir {
		t.Fatalf("@roost_agent_workdir = %q, want %q", got, workdir)
	}
}

func TestActiveTranscriptPath_PrefersReportedPath(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	svc.HandleSessionStart("%0", "agent-1")

	// Before any hook reports a path, ActiveTranscriptPath falls back to a
	// driver-computed path from sess.Project.
	home, _ := os.UserHomeDir()
	wantFallback := filepath.Join(home, ".claude", "projects", "-tmp-proj", "agent-1.jsonl")
	if got := svc.ActiveTranscriptPath(); got != wantFallback {
		t.Fatalf("expected fallback %q, got %q", wantFallback, got)
	}

	// Once the agent reports its real transcript path, that wins over the
	// fallback (worktree case: claude wrote to a different ProjectDir).
	reported := "/home/u/.claude/projects/-tmp-proj--claude-worktrees-foo/agent-1.jsonl"
	svc.HandleAgentTranscriptPath("agent-1", reported)

	if got := svc.ActiveTranscriptPath(); got != reported {
		t.Fatalf("ActiveTranscriptPath = %q, want %q", got, reported)
	}
}

func TestResolveAgentMeta_FallbackToProject(t *testing.T) {
	svc, _, mgr := setupService(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	svc.HandleSessionStart("%0", "agent-1")

	// AgentTranscriptPath is empty, AgentWorkingDir is empty, but we should
	// fall through to driver.TranscriptFilePath(home, sess.Project, agentID).
	// The file does not exist on disk so meta stays empty — but the path
	// resolves cleanly without panicking, which is what matters.
	_ = svc.ResolveAgentMeta()
	agent := svc.AgentStore.GetByWindow(sess.WindowID)
	if agent == nil {
		t.Fatal("expected agent after bind")
	}
	if agent.Title != "" {
		t.Fatalf("expected empty Title without on-disk transcript, got %q", agent.Title)
	}
}

func TestActiveSessionLogPath(t *testing.T) {
	svc, _, mgr := setupService(t)
	if svc.ActiveSessionLogPath() != "" {
		t.Fatal("expected empty path initially")
	}
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	got := svc.ActiveSessionLogPath()
	want := session.LogPath(mgr.DataDir(), sess.ID)
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
