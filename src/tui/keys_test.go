package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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
