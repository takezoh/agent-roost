package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/state"
)

func TestOverlayCardBorderTitle_BothFit(t *testing.T) {
	rendered := fakeCard(60)
	result := overlayCardBorderTitle(rendered, state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, "~/proj", 60, lipgloss.Color("#626262"))
	line0 := strings.Split(result, "\n")[0]
	if !strings.Contains(line0, "claude") {
		t.Error("title missing from border")
	}
	if !strings.Contains(line0, "~/proj") {
		t.Error("badge missing from border")
	}
	if lipgloss.Width(line0) != 60 {
		t.Errorf("line width = %d, want 60", lipgloss.Width(line0))
	}
}

func TestOverlayCardBorderTitle_BadgeTruncated(t *testing.T) {
	longBadge := "/workspace/agent-roost/.claude/worktrees/compressed-honking-sparrow"
	rendered := fakeCard(46)
	result := overlayCardBorderTitle(rendered, state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, longBadge, 46, lipgloss.Color("#626262"))
	line0 := strings.Split(result, "\n")[0]
	if !strings.Contains(line0, "claude") {
		t.Error("title missing from border")
	}
	if !strings.Contains(line0, "…") {
		t.Error("badge should be truncated with ellipsis")
	}
	if lipgloss.Width(line0) != 46 {
		t.Errorf("line width = %d, want 46", lipgloss.Width(line0))
	}
}

func TestOverlayCardBorderTitle_TitleOnlyWhenBadgeTooLong(t *testing.T) {
	// maxBadge = middleW - titleW - 4 = 16 - 9 - 4 = 3 < 4 → badge dropped
	rendered := fakeCard(18)
	result := overlayCardBorderTitle(rendered, state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, "/some/path", 18, lipgloss.Color("#626262"))
	line0 := strings.Split(result, "\n")[0]
	if !strings.Contains(line0, "claude") {
		t.Error("title missing from border")
	}
	if strings.Contains(line0, "/") || strings.Contains(line0, "…") {
		t.Error("badge should be dropped entirely at this width")
	}
	if lipgloss.Width(line0) != 18 {
		t.Errorf("line width = %d, want 18", lipgloss.Width(line0))
	}
}

func TestOverlayCardBorderTitle_TitleTooWide(t *testing.T) {
	rendered := fakeCard(10)
	result := overlayCardBorderTitle(rendered, state.Tag{Text: "verylongcommand"}, state.Tag{}, "", 10, lipgloss.Color("#626262"))
	line0 := strings.Split(result, "\n")[0]
	// Should skip overlay entirely — return original border
	if strings.Contains(line0, "verylongcommand") {
		t.Error("title should not appear when too wide")
	}
}

// fakeCard returns a minimal 3-line card string with border lines of the given width.
func fakeCard(width int) string {
	mid := width - 2
	if mid < 1 {
		mid = 1
	}
	top := "╭" + strings.Repeat("─", mid) + "╮"
	body := "│" + strings.Repeat(" ", mid) + "│"
	bot := "╰" + strings.Repeat("─", mid) + "╯"
	return top + "\n" + body + "\n" + bot
}
