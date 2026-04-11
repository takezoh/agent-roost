package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func TestMainModel_WindowSizeMsg_SetsViewport(t *testing.T) {
	m := NewMainModel(nil)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := model.(MainModel)

	if mm.width != 80 {
		t.Errorf("width = %d, want 80", mm.width)
	}
	if mm.height != 24 {
		t.Errorf("height = %d, want 24", mm.height)
	}

	view := mm.viewport.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 23 {
		t.Errorf("viewport lines = %d, want 23 (height - 1 for title)", len(lines))
	}
}

func TestMainModel_EventUpdatesViewportContent(t *testing.T) {
	m := NewMainModel(nil)
	// Set viewport size first.
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := model.(MainModel)

	sessions := []proto.SessionInfo{
		{ID: "abcdef123456", Project: "proj1", View: state.View{DisplayName: "test-session"}},
	}
	model, _ = mm.handleEvent(proto.EvtSessionsChanged{
		Sessions:   sessions,
		Connectors: nil,
	})
	mm = model.(MainModel)

	content := mm.viewport.GetContent()
	if !strings.Contains(content, "prefix+Space") {
		t.Error("viewport content should contain keybindings")
	}
}

func TestMainModel_ViewContainsTitle(t *testing.T) {
	m := NewMainModel(nil)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	mm := model.(MainModel)

	view := mm.View()
	if !strings.Contains(view.Content, "ROOST") {
		t.Error("view should contain ROOST title")
	}
}

func TestMainModel_ProjectSessionsRendered(t *testing.T) {
	m := NewMainModel(nil)
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	mm := model.(MainModel)

	sessions := []proto.SessionInfo{
		{ID: "abcdef123456", Project: "proj1", View: state.View{DisplayName: "my-session"}},
	}
	model, _ = mm.handleEvent(proto.EvtSessionsChanged{Sessions: sessions})
	mm = model.(MainModel)
	model, _ = mm.handleEvent(proto.EvtProjectSelected{Project: "proj1"})
	mm = model.(MainModel)

	content := mm.viewport.GetContent()
	if !strings.Contains(content, "abcdef") {
		t.Errorf("viewport should contain session ID prefix, got: %q", content)
	}
}
