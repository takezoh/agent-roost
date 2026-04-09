package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/state"
)

func TestMainDisconnectQuitsProgram(t *testing.T) {
	m := NewMainModel(nil)
	result, cmd := m.Update(mainDisconnectMsg{})
	if result == nil {
		t.Fatal("expected non-nil model")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd (tea.Quit)")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected QuitMsg, got %T", msg)
	}
}

func TestMainSessionsChangedUpdatesModel(t *testing.T) {
	m := NewMainModel(nil)
	event := core.NewEvent("sessions-changed")
	event.Sessions = []core.SessionInfo{
		{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
	}

	result, _ := m.Update(mainEventMsg(event))
	model := result.(MainModel)
	if len(model.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(model.sessions))
	}
}

func TestMainProjectSelectedUpdatesModel(t *testing.T) {
	m := NewMainModel(nil)
	event := core.NewEvent("project-selected")
	event.SelectedProject = "/tmp/proj"

	result, _ := m.Update(mainEventMsg(event))
	model := result.(MainModel)
	if model.selectedProject != "/tmp/proj" {
		t.Fatalf("expected /tmp/proj, got %s", model.selectedProject)
	}
}

func TestMainViewShowsKeybindings(t *testing.T) {
	m := NewMainModel(nil)
	m.width = 80
	m.height = 24
	view := m.View()
	content := view.Content
	if !strings.Contains(content, "prefix+Space") {
		t.Fatal("expected keybinding help in view")
	}
	if !strings.Contains(content, "Palette") {
		t.Fatal("expected Palette in view")
	}
}

func TestMainViewShowsProjectSessions(t *testing.T) {
	m := NewMainModel(nil)
	m.width = 80
	m.height = 24
	m.selectedProject = "/tmp/proj"
	m.sessions = []core.SessionInfo{
		{ID: "abc123def", Project: "/tmp/proj", Command: "claude", WindowID: "@1", State: state.StatusRunning, CreatedAt: "2025-01-01T00:00:00Z"},
		{ID: "xyz789ghi", Project: "/other", Command: "gemini", WindowID: "@2", State: state.StatusIdle, CreatedAt: "2025-01-01T00:00:00Z"},
	}
	view := m.View()
	content := view.Content
	if !strings.Contains(content, "abc123") {
		t.Fatal("expected session abc123 in view")
	}
	if strings.Contains(content, "xyz789") {
		t.Fatal("should not show session from different project")
	}
}
