package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/state"
)

func TestOverlayCardBorderTitle_BothFit(t *testing.T) {
	rendered := fakeCard(60)
	result := overlayCardBorderTitle(rendered, "", state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, "~/proj", 60, lipgloss.Color("#626262"), false)
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
	result := overlayCardBorderTitle(rendered, "", state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, longBadge, 46, lipgloss.Color("#626262"), false)
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
	result := overlayCardBorderTitle(rendered, "", state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, "/some/path", 18, lipgloss.Color("#626262"), false)
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
	result := overlayCardBorderTitle(rendered, "", state.Tag{Text: "verylongcommand"}, state.Tag{}, "", 10, lipgloss.Color("#626262"), false)
	line0 := strings.Split(result, "\n")[0]
	// Should skip overlay entirely — return original border
	if strings.Contains(line0, "verylongcommand") {
		t.Error("title should not appear when too wide")
	}
}

func TestOverlayCardBorderTitle_IconBeforeTitle(t *testing.T) {
	rendered := fakeCard(60)
	result := overlayCardBorderTitle(rendered, "●", state.Tag{Text: "claude", Background: "#D97757", Foreground: "#FFFFFF"}, state.Tag{}, "", 60, lipgloss.Color("#626262"), false)
	line0 := strings.Split(result, "\n")[0]
	iconIdx := strings.Index(line0, "●")
	titleIdx := strings.Index(line0, "claude")
	if iconIdx < 0 || titleIdx < 0 || iconIdx > titleIdx {
		t.Errorf("icon must precede title; line=%q", line0)
	}
	if lipgloss.Width(line0) != 60 {
		t.Errorf("line width = %d, want 60", lipgloss.Width(line0))
	}
}

func TestOverlayCardBorderTitle_IconOnly(t *testing.T) {
	rendered := fakeCard(20)
	result := overlayCardBorderTitle(rendered, "●", state.Tag{}, state.Tag{}, "", 20, lipgloss.Color("#626262"), false)
	line0 := strings.Split(result, "\n")[0]
	if !strings.Contains(line0, "●") {
		t.Error("icon missing from border")
	}
	if lipgloss.Width(line0) != 20 {
		t.Errorf("line width = %d, want 20", lipgloss.Width(line0))
	}
}

func TestOverlayCardBorderTitleSecondaryDimmed(t *testing.T) {
	rendered := fakeCard(60)
	secondary := state.Tag{Text: "aider", Background: "#AA0000", Foreground: "#FFFFFF"}
	// secondaryDim=false: chip appears with its custom background (tagStyle wraps it)
	resultOn := overlayCardBorderTitle(rendered, "", state.Tag{Text: "claude"}, secondary, "", 60, lipgloss.Color("#626262"), false)
	// secondaryDim=true: chip text still appears but tagStyle background should NOT be applied
	resultDim := overlayCardBorderTitle(rendered, "", state.Tag{Text: "claude"}, secondary, "", 60, lipgloss.Color("#626262"), true)
	line0On := strings.Split(resultOn, "\n")[0]
	line0Dim := strings.Split(resultDim, "\n")[0]
	if !strings.Contains(line0On, "aider") {
		t.Error("non-dim: secondary text must appear")
	}
	if !strings.Contains(line0Dim, "aider") {
		t.Error("dim: secondary text must still appear")
	}
	// The rendered strings must differ: one uses tagStyle (with background ANSI),
	// the other uses mutedStyle. Identical output would mean one path is dead code.
	if line0On == line0Dim {
		t.Error("dim and non-dim must produce different rendered strings")
	}
	// tagStyle adds 1-cell padding on each side; mutedStyle does not.
	// The dim line must therefore be narrower than the non-dim line.
	if lipgloss.Width(line0Dim) >= lipgloss.Width(line0On) {
		t.Errorf("dim line width %d must be less than non-dim line width %d (tagStyle adds padding)", lipgloss.Width(line0Dim), lipgloss.Width(line0On))
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
