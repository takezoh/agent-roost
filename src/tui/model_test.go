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

func TestCursorNavigatesItems(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj2", Command: "claude", WindowID: "@2"},
	}
	m.rebuildItems()
	// items: [proj1(0), sess1(1), proj2(2), sess2(3)]

	if len(m.items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(m.items))
	}

	// cursor=0 -> project row, cursorSession() == nil
	m.cursor = 0
	if m.cursorSession() != nil {
		t.Fatal("cursor on project row should return nil session")
	}
	if m.cursorProjectName() != "proj1" {
		t.Fatalf("expected proj1, got %s", m.cursorProjectName())
	}

	// Down -> cursor=1 (session1)
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model := result.(Model)
	if model.cursor != 1 {
		t.Fatalf("expected cursor=1, got %d", model.cursor)
	}
	if model.cursorSession() == nil || model.cursorSession().ID != "aaa111" {
		t.Fatal("cursor=1 should point to session aaa111")
	}

	// Down -> cursor=2 (project2)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = result.(Model)
	if model.cursor != 2 {
		t.Fatalf("expected cursor=2, got %d", model.cursor)
	}
	if model.cursorSession() != nil {
		t.Fatal("cursor=2 should be project row")
	}

	// Down -> cursor=3 (session2)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = result.(Model)
	if model.cursor != 3 {
		t.Fatalf("expected cursor=3, got %d", model.cursor)
	}
	if model.cursorSession() == nil || model.cursorSession().ID != "bbb222" {
		t.Fatal("cursor=3 should point to session bbb222")
	}

	// Down at bottom -> stays at 3
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	model = result.(Model)
	if model.cursor != 3 {
		t.Fatalf("cursor should stay at 3, got %d", model.cursor)
	}

	// Up -> cursor=2 (project2)
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = result.(Model)
	if model.cursor != 2 {
		t.Fatalf("expected cursor=2, got %d", model.cursor)
	}

	// Up to top -> cursor=0
	model.cursor = 0
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	model = result.(Model)
	if model.cursor != 0 {
		t.Fatalf("cursor should stay at 0, got %d", model.cursor)
	}
}

func TestRebuildItemsCursorOnProject(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "abc123", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
	}
	m.rebuildItems()
	// items: [proj(0), sess(1)], cursor=0 is project row
	if m.cursor != 0 {
		t.Fatalf("expected cursor=0, got %d", m.cursor)
	}
	if !m.items[0].isProject {
		t.Fatal("items[0] should be project row")
	}
	if m.cursorSession() != nil {
		t.Fatal("cursor on project row should return nil session")
	}
}

func TestFoldedProjectNavigable(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj2", Command: "claude", WindowID: "@2"},
	}
	m.folded["proj1"] = true
	m.rebuildItems()
	// items: [proj1(0), proj2(1), sess2(2)]

	if len(m.items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(m.items))
	}

	m.cursor = 0
	if m.cursorProjectName() != "proj1" {
		t.Fatalf("expected proj1, got %s", m.cursorProjectName())
	}

	// Tab to unfold proj1
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := result.(Model)
	// items: [proj1(0), sess1(1), proj2(2), sess2(3)]
	if len(model.items) != 4 {
		t.Fatalf("expected 4 items after unfold, got %d", len(model.items))
	}
	// cursor should stay on proj1
	if model.cursor != 0 {
		t.Fatalf("expected cursor=0 after unfold, got %d", model.cursor)
	}
}

func TestToggleFromProjectRow(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
	}
	m.rebuildItems()
	// items: [proj1(0), sess1(1)]

	m.cursor = 0 // on project row
	result, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model := result.(Model)
	if !model.folded["proj1"] {
		t.Fatal("proj1 should be folded after toggle")
	}
	// items: [proj1(0)]
	if len(model.items) != 1 {
		t.Fatalf("expected 1 item after fold, got %d", len(model.items))
	}

	// Toggle again to unfold
	result, _ = model.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	model = result.(Model)
	if model.folded["proj1"] {
		t.Fatal("proj1 should be unfolded after second toggle")
	}
	if len(model.items) != 2 {
		t.Fatalf("expected 2 items after unfold, got %d", len(model.items))
	}
}

func TestRowToItemIndex(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.width = 60
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj", Command: "claude", WindowID: "@2", LastPrompt: "hello"},
	}
	m.rebuildItems()

	// Render to populate rows cache.
	m.View()

	// Layout: panel top border is at y=0. Items start at y=1.
	// The first row (y=0) and any row past the last item should return -1.
	if got := m.rowToItemIndex(0); got != -1 {
		t.Errorf("rowToItemIndex(0) = %d, want -1 (panel top border)", got)
	}

	// Walk through items using their cached row counts.
	row := sessionsHeaderRows
	for i, item := range m.items {
		if item.rows <= 0 {
			t.Fatalf("item %d has zero rows; View did not populate cache", i)
		}
		// First and last row of each item should map to that item.
		if got := m.rowToItemIndex(row); got != i {
			t.Errorf("rowToItemIndex(%d) first row of item %d = %d, want %d", row, i, got, i)
		}
		if got := m.rowToItemIndex(row + item.rows - 1); got != i {
			t.Errorf("rowToItemIndex(%d) last row of item %d = %d, want %d", row+item.rows-1, i, got, i)
		}
		row += item.rows
	}

	// Just past the last item should fall outside.
	if got := m.rowToItemIndex(row); got != -1 {
		t.Errorf("rowToItemIndex(%d) past last item = %d, want -1", row, got)
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

func TestFirstSessionIndex(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
	}
	m.rebuildItems()
	// items: [proj1(0), sess1(1)]
	if got := m.firstSessionIndex(); got != 1 {
		t.Fatalf("expected firstSessionIndex()=1, got %d", got)
	}

	// All folded -> no session rows -> returns 0
	m.folded["proj1"] = true
	m.rebuildItems()
	if got := m.firstSessionIndex(); got != 0 {
		t.Fatalf("expected firstSessionIndex()=0 when all folded, got %d", got)
	}
}

func TestRebuildItemsPreservesCursor(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj1", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj2", Command: "claude", WindowID: "@2"},
	}
	m.rebuildItems()
	// items: [proj1(0), sess1(1), proj2(2), sess2(3)]

	// cursor on sess2
	m.cursor = 3
	m.rebuildItems()
	if m.cursor != 3 {
		t.Fatalf("expected cursor=3 preserved, got %d", m.cursor)
	}

	// cursor on proj2, fold proj1
	m.cursor = 2
	m.folded["proj1"] = true
	m.rebuildItems()
	// items: [proj1(0), proj2(1), sess2(2)]
	if m.cursor != 1 || m.items[m.cursor].project != "proj2" {
		t.Fatalf("expected cursor on proj2 after fold, cursor=%d project=%s", m.cursor, m.items[m.cursor].project)
	}
}

