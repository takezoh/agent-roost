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

func TestCursorNavigatesSessions(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj2", Command: "claude", WindowID: "@2"},
	}
	m.rebuildItems()
	// visible: [session1, session2], cursor indexes into visible

	// initial cursor is on a session
	if m.cursorSession() == nil {
		t.Fatal("initial cursor must point to a session")
	}

	// cursor=0 (session1); Down -> cursor=1 (session2)
	m.cursor = 0
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model := result.(Model)
	if model.cursorSession() == nil {
		t.Fatal("cursor after Down must point to a session")
	}
	if model.cursorSession().ID != "bbb222" {
		t.Fatalf("expected bbb222, got %s", model.cursorSession().ID)
	}

	// cursor=1 (session2); Up -> cursor=0 (session1)
	model.cursor = 1
	result2, _ := model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model2 := result2.(Model)
	if model2.cursorSession() == nil {
		t.Fatal("cursor after Up must point to a session")
	}
	if model2.cursorSession().ID != "aaa111" {
		t.Fatalf("expected aaa111, got %s", model2.cursorSession().ID)
	}

	// cursor=0 (topmost); Up should not move
	model2.cursor = 0
	result3, _ := model2.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model3 := result3.(Model)
	if model3.cursor != 0 {
		t.Fatalf("cursor should stay at 0, got %d", model3.cursor)
	}
}

func TestRebuildItemsCursorOnSession(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
	}
	m.rebuildItems()
	if m.cursorSession() == nil {
		t.Fatal("cursor after rebuildItems must point to a session")
	}
	if m.cursorSession().ID != "abc123" {
		t.Fatalf("expected abc123, got %s", m.cursorSession().ID)
	}
}

func TestRowToItemIndex(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj", Command: "claude", WindowID: "@2", LastPrompt: "hello"},
	}
	m.rebuildItems()

	// Render to populate rows cache.
	m.View()

	// Header: 2 rows (row 0: "SESSIONS", row 1: blank)
	// Row 2: project header (1 row)
	// Row 3-4: session aaa111 (no LastPrompt → 2 rows)
	// Row 5-7: session bbb222 (has LastPrompt → 3 rows)

	tests := []struct {
		y    int
		want int // expected item index, -1 for outside
	}{
		{0, -1},  // header
		{1, -1},  // blank
		{2, 0},   // project
		{3, 1},   // session aaa111 line1
		{4, 1},   // session aaa111 line2 (tags)
		{5, 2},   // session bbb222 line1
		{6, 2},   // session bbb222 line2 (lastPrompt)
		{7, 2},   // session bbb222 line3 (tags)
		{8, -1},  // outside
	}
	for _, tt := range tests {
		got := m.rowToItemIndex(tt.y)
		if got != tt.want {
			t.Errorf("rowToItemIndex(%d) = %d, want %d", tt.y, got, tt.want)
		}
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
