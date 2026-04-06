package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
)

func TestDisconnectMsgQuitsProgram(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	result, cmd := m.Update(disconnectMsg{})
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

func TestSessionsChangedUpdatesModel(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	event := core.NewEvent("sessions-changed")
	event.Sessions = []core.SessionInfo{
		{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
	}

	result, _ := m.Update(serverEventMsg(event))
	model := result.(Model)
	if len(model.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(model.sessions))
	}
	if model.sessions[0].ID != "abc123" {
		t.Fatalf("expected abc123, got %s", model.sessions[0].ID)
	}
	if len(model.items) != 2 {
		t.Fatalf("expected 2 items (project+session), got %d", len(model.items))
	}
}

func TestStatesUpdatedPreservesExistingSessions(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
	}
	m.rebuildItems()

	event := core.NewEvent("states-updated")
	event.States = map[string]session.State{"@1": session.StateWaiting}

	result, _ := m.Update(serverEventMsg(event))
	model := result.(Model)
	if len(model.sessions) != 1 {
		t.Fatal("sessions should be preserved")
	}
	if model.sessions[0].State != session.StateWaiting {
		t.Fatalf("expected Waiting, got %s", model.sessions[0].State)
	}
}
