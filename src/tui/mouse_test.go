package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func TestDeactivateDoneMsgClearsState(t *testing.T) {
	m := Model{
		active:   "@1",
		anchored: "@1",
		folded:   make(map[string]bool),
	}
	result, cmd := m.Update(deactivateDoneMsg{err: nil})
	got := result.(Model)
	if got.active != "" {
		t.Errorf("active = %q, want empty", got.active)
	}
	if got.anchored != "" {
		t.Errorf("anchored = %q, want empty", got.anchored)
	}
	if cmd == nil {
		t.Error("expected focusCmd, got nil")
	}
}

func TestDeactivateDoneMsgPreservesStateOnError(t *testing.T) {
	m := Model{
		active:   "@1",
		anchored: "@1",
		folded:   make(map[string]bool),
	}
	result, cmd := m.Update(deactivateDoneMsg{err: errDummy})
	got := result.(Model)
	if got.active != "@1" {
		t.Errorf("active = %q, want @1 (preserved on error)", got.active)
	}
	if got.anchored != "@1" {
		t.Errorf("anchored = %q, want @1 (preserved on error)", got.anchored)
	}
	if cmd == nil {
		t.Error("expected focusCmd even on error, got nil")
	}
}

var errDummy = fmt.Errorf("dummy")

func TestClickHeaderWithActiveSession(t *testing.T) {
	m := Model{
		active: "@1",
		height: 20,
		width:  80,
		folded: make(map[string]bool),
		filter: allOnFilter(),
	}
	msg := tea.MouseClickMsg(tea.Mouse{X: 5, Y: 0, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd == nil {
		t.Fatal("expected deactivateCmd, got nil")
	}
}

func TestClickHeaderWithoutActiveSession(t *testing.T) {
	m := Model{
		active: "",
		height: 20,
		width:  80,
		folded: make(map[string]bool),
		filter: allOnFilter(),
	}
	msg := tea.MouseClickMsg(tea.Mouse{X: 5, Y: 0, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd == nil {
		t.Fatal("expected focusCmd, got nil")
	}
}

func TestClickConnectorSummaryWithActiveSession(t *testing.T) {
	m := Model{
		active: "@1",
		connectors: []proto.ConnectorInfo{
			{Summary: "2 PRs"},
		},
		height: 20,
		width:  80,
		folded: make(map[string]bool),
		filter: allOnFilter(),
	}
	// Connector summary is at row headerRowCount()-1 = 3 (with connector: 4-1=3)
	summaryRow := m.headerRowCount() - 1
	msg := tea.MouseClickMsg(tea.Mouse{X: 5, Y: summaryRow, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd == nil {
		t.Fatal("expected deactivateCmd, got nil")
	}
}

func TestClickConnectorSummaryWithoutActiveSession(t *testing.T) {
	m := Model{
		active: "",
		connectors: []proto.ConnectorInfo{
			{Summary: "2 PRs"},
		},
		height: 20,
		width:  80,
		folded: make(map[string]bool),
		filter: allOnFilter(),
	}
	summaryRow := m.headerRowCount() - 1
	msg := tea.MouseClickMsg(tea.Mouse{X: 5, Y: summaryRow, Button: tea.MouseLeft})
	_, cmd := m.handleMouseClick(msg)
	if cmd == nil {
		t.Fatal("expected focusCmd, got nil")
	}
}

func TestHandleServerEventClearsMissingActiveAndAnchor(t *testing.T) {
	m := Model{
		active:   "gone",
		anchored: "gone",
		folded:   make(map[string]bool),
		filter:   allOnFilter(),
		sessions: []proto.SessionInfo{
			{ID: "gone", Project: "/tmp/p", View: state.View{DisplayName: "p"}},
		},
	}
	m.rebuildItems()

	result, _ := m.handleServerEvent(proto.EvtSessionsChanged{
		Sessions: []proto.SessionInfo{
			{ID: "keep", Project: "/tmp/p", View: state.View{DisplayName: "p"}},
		},
	})
	got := result.(Model)
	if got.active != "" {
		t.Errorf("active = %q, want empty", got.active)
	}
	if got.anchored != "" {
		t.Errorf("anchored = %q, want empty", got.anchored)
	}
}
