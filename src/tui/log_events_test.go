package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
)

// stubOpen swaps openProject for the duration of a test and records
// every call. The returned closure restores the previous function.
func stubOpen(t *testing.T) (*[]string, func()) {
	t.Helper()
	prev := openProject
	var calls []string
	openProject = func(target string) error {
		calls = append(calls, target)
		return nil
	}
	return &calls, func() { openProject = prev }
}

func newInfoLogModel(project string) LogModel {
	m := LogModel{
		currentSession: &proto.SessionInfo{ID: "sess-1", Project: project},
		tabs:           []*tabState{{label: "INFO", kind: tabKindInfo}},
		activeTab:      0,
		width:          80,
		height:         24,
	}
	m.viewport.SetWidth(80)
	m.viewport.SetHeight(23)
	// Populate viewport via the same path runtime uses.
	m.renderInfoTab()
	return m
}

func TestHandleEnterOpensProjectOnInfoTab(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("/home/user/proj")
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected tea.Cmd, got nil")
	}
	_ = cmd() // invoke the command to trigger openProject
	if len(*calls) != 1 || (*calls)[0] != "/home/user/proj" {
		t.Errorf("calls = %v, want [/home/user/proj]", *calls)
	}
}

func TestHandleEnterNoopWithoutProject(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("")
	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Even without a project, handleKey still forwards Enter to the
	// viewport for scrolling; what matters is that openProject is not
	// called.
	if cmd != nil {
		_ = cmd()
	}
	if len(*calls) != 0 {
		t.Errorf("openProject should not be called, got %v", *calls)
	}
}

func TestHandleEnterNoopOutsideInfoTab(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("/home/user/proj")
	m.tabs = []*tabState{{label: "LOG", kind: tabKindLog}}
	m.activeTab = 0

	_, cmd := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		_ = cmd()
	}
	if len(*calls) != 0 {
		t.Errorf("openProject should not be called outside INFO tab, got %v", *calls)
	}
}

func TestHandleClickOpensProjectOnProjectRow(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("/home/user/proj")
	if m.projectLine < 0 {
		t.Fatalf("fixture did not compute projectLine, got %d", m.projectLine)
	}
	// Body row N is rendered at screen y = N + 1 (1 header row).
	clickY := m.projectLine + 1
	msg := tea.MouseClickMsg(tea.Mouse{X: 10, Y: clickY, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd == nil {
		t.Fatal("expected tea.Cmd, got nil")
	}
	_ = cmd()
	if len(*calls) != 1 || (*calls)[0] != "/home/user/proj" {
		t.Errorf("calls = %v, want [/home/user/proj]", *calls)
	}
}

func TestHandleClickOnNonProjectRowNoop(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("/home/user/proj")
	// Click one row below the Project row.
	clickY := m.projectLine + 2
	msg := tea.MouseClickMsg(tea.Mouse{X: 10, Y: clickY, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd != nil {
		_ = cmd()
	}
	if len(*calls) != 0 {
		t.Errorf("openProject should not be called on non-project row, got %v", *calls)
	}
}

func TestHandleClickOnProjectRowOutsideInfoNoop(t *testing.T) {
	calls, restore := stubOpen(t)
	defer restore()

	m := newInfoLogModel("/home/user/proj")
	m.tabs = []*tabState{{label: "LOG", kind: tabKindLog}}
	m.activeTab = 0

	msg := tea.MouseClickMsg(tea.Mouse{X: 10, Y: 2, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd != nil {
		_ = cmd()
	}
	if len(*calls) != 0 {
		t.Errorf("openProject should not fire outside INFO, got %v", *calls)
	}
}
