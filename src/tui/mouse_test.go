package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/state"
)

func setupModelWithSessions() Model {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{ID: "aaa111", Project: "/tmp/proj", Command: "claude", WindowID: "@1"},
		{ID: "bbb222", Project: "/tmp/proj", Command: "claude", WindowID: "@2"},
	}
	m.rebuildItems()
	// items: [proj(0), sess1(1), sess2(2)]
	// Set anchored to sess1
	m.cursor = 1
	m.anchored = "@1"
	m.active = "@1"
	m.width = 40
	m.height = 20
	return m
}

func TestMouseLeaveRevertsToAnchor(t *testing.T) {
	m := setupModelWithSessions()

	// Simulate hover on sess2: update cursor, set hovering
	m.cursor = 2
	m.hovering = true
	m.active = "@2"
	m.mouseSeq = 5
	m.lastMouseX = 38 // within edge margin (width=40, margin=3)
	m.lastMouseY = 10

	// Send mouseLeaveMsg with matching seq
	result, cmd := m.Update(mouseLeaveMsg{seq: 5})
	model := result.(Model)

	if model.hovering {
		t.Fatal("hovering should be false after mouseLeave")
	}
	if model.cursor != 1 {
		t.Fatalf("expected cursor reverted to 1, got %d", model.cursor)
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd to revert preview")
	}
}

func TestMouseLeaveIgnoredWhenNotAtEdge(t *testing.T) {
	m := setupModelWithSessions()
	m.cursor = 2
	m.hovering = true
	m.active = "@2"
	m.mouseSeq = 5
	m.lastMouseX = 20 // center of pane
	m.lastMouseY = 10

	result, cmd := m.Update(mouseLeaveMsg{seq: 5})
	model := result.(Model)

	// Should NOT revert because mouse is not at edge
	if model.cursor != 2 {
		t.Fatalf("cursor should stay at 2, got %d", model.cursor)
	}
	if cmd != nil {
		t.Fatal("expected nil cmd when mouse not at edge")
	}
}

func TestMouseLeaveStaleSeqIgnored(t *testing.T) {
	m := setupModelWithSessions()
	m.hovering = true
	m.mouseSeq = 10
	m.cursor = 2
	m.active = "@2"

	// Stale seq
	result, cmd := m.Update(mouseLeaveMsg{seq: 5})
	model := result.(Model)

	if !model.hovering {
		t.Fatal("hovering should remain true for stale seq")
	}
	if model.cursor != 2 {
		t.Fatal("cursor should not change for stale seq")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd for stale seq")
	}
}

func TestClickSetsAnchor(t *testing.T) {
	m := setupModelWithSessions()
	m.View() // populate row cache

	// Click on sess2 (row depends on rendering; use rowToItemIndex to find it)
	sess2Row := -1
	for y := 0; y < 20; y++ {
		if m.rowToItemIndex(y) == 2 {
			sess2Row = y
			break
		}
	}
	if sess2Row < 0 {
		t.Fatal("could not find row for sess2")
	}

	result, _ := m.Update(tea.MouseClickMsg{X: 1, Y: sess2Row, Button: tea.MouseLeft})
	model := result.(Model)

	if model.anchored != "@2" {
		t.Fatalf("expected anchored=@2, got %s", model.anchored)
	}
}

func TestFilterChipClickTogglesFilter(t *testing.T) {
	m := setupModelWithSessions()
	// Mark sessions with distinct states so the chips have meaningful counts.
	m.sessions[0].State = state.StatusRunning
	m.sessions[1].State = state.StatusIdle
	m.rebuildItems()
	m.View() // populate row cache (also exercises filter bar layout)

	// Click the middle of the idle chip (index 2).
	_, boxes := filterBarLayout(m.filter)
	idleBox := boxes[2]
	x := (idleBox.x0 + idleBox.x1) / 2

	result, _ := m.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	model := result.(Model)
	if model.filter.matches(state.StatusIdle) {
		t.Fatal("idle should be off after clicking idle chip")
	}
	if got := countSessions(model.items); got != 1 {
		t.Fatalf("expected 1 visible session after click, got %d", got)
	}

	// Click the All chip — partial state, so it should reset to all-on.
	_, boxes = filterBarLayout(model.filter)
	allBox := boxes[len(boxes)-1]
	x = (allBox.x0 + allBox.x1) / 2
	result, _ = model.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	model = result.(Model)
	if !model.filter.allOn() {
		t.Fatal("filter should be all-on after clicking All from partial state")
	}

	// Click the All chip again — now all-on, so it should clear every chip.
	_, boxes = filterBarLayout(model.filter)
	allBox = boxes[len(boxes)-1]
	x = (allBox.x0 + allBox.x1) / 2
	result, _ = model.Update(tea.MouseClickMsg{X: x, Y: 1, Button: tea.MouseLeft})
	model = result.(Model)
	if model.filter.anyOn() {
		t.Fatal("filter should be all-off after clicking All from all-on state")
	}
	if got := countSessions(model.items); got != 0 {
		t.Fatalf("expected 0 visible sessions after all-off, got %d", got)
	}
}

func TestMouseLeaveNoAnchor(t *testing.T) {
	m := setupModelWithSessions()
	m.anchored = ""
	m.hovering = true
	m.mouseSeq = 1
	m.cursor = 2
	m.active = "@2"

	result, cmd := m.Update(mouseLeaveMsg{seq: 1})
	model := result.(Model)

	if model.hovering {
		t.Fatal("hovering should be false")
	}
	// No revert since no anchor
	if cmd != nil {
		t.Fatal("expected nil cmd when no anchor")
	}
	if model.cursor != 2 {
		t.Fatal("cursor should not change when no anchor")
	}
}
