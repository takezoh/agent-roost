package core

import (
	"testing"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/tmux"
)

type mockPaneOp struct {
	chainCalls  [][]string
	selectCalls []string
}

func (m *mockPaneOp) SwapPane(src, dst string) error                { return nil }
func (m *mockPaneOp) SelectPane(target string) error                { m.selectCalls = append(m.selectCalls, target); return nil }
func (m *mockPaneOp) RespawnPane(target, cmd string) error          { return nil }
func (m *mockPaneOp) RunChain(cmds ...[]string) error               { m.chainCalls = cmds; return nil }
func (m *mockPaneOp) WindowIDFromPane(paneID string) (string, error) { return "@0", nil }

type mockTmuxForService struct {
	nextID  string
	windows map[string]bool
}

func (m *mockTmuxForService) NewWindow(name, command, startDir string) (string, error) {
	id := m.nextID
	m.windows[id] = true
	return id, nil
}
func (m *mockTmuxForService) KillWindow(windowID string) error   { delete(m.windows, windowID); return nil }
func (m *mockTmuxForService) ListWindowIDs() ([]string, error) {
	var ids []string
	for id := range m.windows { ids = append(ids, id) }
	return ids, nil
}
func (m *mockTmuxForService) SetOption(target, key, value string) error { return nil }
func (m *mockTmuxForService) PipePane(target, command string) error     { return nil }

type mockCapturer struct{ content map[string]string }

func (m *mockCapturer) CapturePaneLines(target string, n int) (string, error) {
	return m.content[target], nil
}

func setupService(t *testing.T) (*Service, *mockPaneOp, *session.Manager) {
	t.Helper()
	mt := &mockTmuxForService{nextID: "@1", windows: make(map[string]bool)}
	mgr := session.NewManager(mt, t.TempDir())
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, mon, panes, "roost", "")
	return svc, panes, mgr
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
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, mon, panes, "roost", "")

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
	mon := tmux.NewMonitor(&mockCapturer{content: map[string]string{}}, 30, nil)
	panes := &mockPaneOp{}
	svc := NewService(mgr, mon, panes, "roost", "@5")
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
