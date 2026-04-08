package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
)

func TestKeyboardNavSetsAnchor(t *testing.T) {
	m := setupModelWithSessions()
	m.cursor = 1 // on sess1
	m.anchored = "@1"

	// Down -> cursor=2 (sess2)
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model := result.(Model)

	if model.anchored != "@2" {
		t.Fatalf("expected anchored=@2 after Down, got %s", model.anchored)
	}
	if model.hovering {
		t.Fatal("hovering should be false after keyboard nav")
	}
}

func TestKeyPressDuringHoverClearsHovering(t *testing.T) {
	m := setupModelWithSessions()
	m.hovering = true
	m.mouseSeq = 5
	m.cursor = 1

	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model := result.(Model)

	if model.hovering {
		t.Fatal("hovering should be cleared on key press")
	}
}

func filterTestModel() Model {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa", Project: "/tmp/proj", Command: "claude", WindowID: "@1", State: session.StateRunning},
		{ID: "bbb", Project: "/tmp/proj", Command: "claude", WindowID: "@2", State: session.StateIdle},
	}
	m.rebuildItems()
	return m
}

func TestFilterKeyTogglesStatus(t *testing.T) {
	m := filterTestModel()
	if got := countSessions(m.items); got != 2 {
		t.Fatalf("expected 2 visible sessions before filter, got %d", got)
	}

	// Press "3" → toggle idle off.
	result, _ := m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model := result.(Model)
	if model.filter.matches(session.StateIdle) {
		t.Fatal("idle should be off after pressing 3")
	}
	if got := countSessions(model.items); got != 1 {
		t.Fatalf("expected 1 visible session after filter, got %d", got)
	}

	// Press "3" again → toggle idle back on.
	result, _ = model.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	model = result.(Model)
	if !model.filter.matches(session.StateIdle) {
		t.Fatal("idle should be on after second 3 press")
	}
}

func TestFilterResetKey(t *testing.T) {
	m := filterTestModel()
	m.filter = statusFilter{true, false, false, false, false}
	m.rebuildItems()

	result, _ := m.Update(tea.KeyPressMsg{Code: '0', Text: "0"})
	model := result.(Model)
	if !model.filter.allOn() {
		t.Fatal("filter should be all-on after pressing 0")
	}
	if got := countSessions(model.items); got != 2 {
		t.Fatalf("expected 2 visible sessions after reset, got %d", got)
	}
}
