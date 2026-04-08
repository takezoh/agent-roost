package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

func TestPanelWrapsBodyWithBorder(t *testing.T) {
	out := Panel("ROOST", "3 sessions", "hello world", 40)
	lines := strings.Split(out, "\n")

	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines, got %d", len(lines))
	}

	// Visible width of every rendered line should be equal (lipgloss pads).
	widths := make(map[int]int)
	for _, l := range lines {
		widths[lipgloss.Width(l)]++
	}
	if len(widths) != 1 {
		t.Errorf("expected all lines to have equal visible width, got %v", widths)
	}

	// Top border line should contain both title and badge (stripped of ANSI).
	plainTop := stripANSI(lines[0])
	if !strings.Contains(plainTop, "ROOST") {
		t.Errorf("top line missing title: %q", plainTop)
	}
	if !strings.Contains(plainTop, "3 sessions") {
		t.Errorf("top line missing badge: %q", plainTop)
	}
	if !strings.Contains(plainTop, "╭") || !strings.Contains(plainTop, "╮") {
		t.Errorf("top line missing corners: %q", plainTop)
	}

	// Bottom line should be a plain border line with corners.
	plainBot := stripANSI(lines[len(lines)-1])
	if !strings.Contains(plainBot, "╰") || !strings.Contains(plainBot, "╯") {
		t.Errorf("bottom line missing corners: %q", plainBot)
	}
}

func TestPanelChromeRowsConstant(t *testing.T) {
	out := Panel("X", "", "body", 30)
	lines := strings.Split(out, "\n")
	// body is single line → total = 1 + PanelChromeRows
	if len(lines) != 1+PanelChromeRows {
		t.Errorf("expected %d lines, got %d", 1+PanelChromeRows, len(lines))
	}
}

func TestPanelClampsNarrowWidth(t *testing.T) {
	out := Panel("TITLE", "B", "content", 5)
	if out == "" {
		t.Fatal("expected non-empty output even for tiny width")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected ≥3 lines, got %d", len(lines))
	}
}

func TestCardAddsBorder(t *testing.T) {
	body := "● running"
	out := Card(body, false, 20)
	if !strings.Contains(stripANSI(out), "running") {
		t.Errorf("card body missing: %q", out)
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Errorf("expected at least 3 lines (top+body+bot), got %d", len(lines))
	}
}

func TestCardSelectedDiffersFromUnselected(t *testing.T) {
	sel := Card("x", true, 20)
	unsel := Card("x", false, 20)
	if sel == unsel {
		t.Error("expected selected card to differ from unselected")
	}
}

func TestFooterRendersBindings(t *testing.T) {
	b1 := key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	b2 := key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "stop"))
	out := Footer([]key.Binding{b1, b2})
	plain := stripANSI(out)
	if !strings.Contains(plain, "n") || !strings.Contains(plain, "new") {
		t.Errorf("footer missing n/new: %q", plain)
	}
	if !strings.Contains(plain, "d") || !strings.Contains(plain, "stop") {
		t.Errorf("footer missing d/stop: %q", plain)
	}
}

func TestFooterEmptyBindings(t *testing.T) {
	if Footer(nil) != "" {
		t.Error("expected empty string for nil bindings")
	}
}

// stripANSI removes ANSI escape sequences for assertion-friendly comparison.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
