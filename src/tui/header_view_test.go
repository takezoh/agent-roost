package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/proto"
)

func TestFrameTabLayoutFrameFocusedHighlightsActive(t *testing.T) {
	sess := proto.SessionInfo{
		ID:            "s1",
		ActiveFrameID: "f2",
		Frames: []proto.FrameInfo{
			{ID: "f1", Command: "claude"},
			{ID: "f2", Command: "aider"},
		},
	}
	line, _ := frameTabLayout(sess, true)
	// Active frame "f2" rendered as "[aider]" with bracket markers.
	if !strings.Contains(stripANSI(line), "[aider]") {
		t.Errorf("frameFocused=true: expected [aider] in output; got %q", stripANSI(line))
	}
}

func TestFrameTabLayoutNotFrameFocusedAllDimmed(t *testing.T) {
	sess := proto.SessionInfo{
		ID:            "s1",
		ActiveFrameID: "f2",
		Frames: []proto.FrameInfo{
			{ID: "f1", Command: "claude"},
			{ID: "f2", Command: "aider"},
		},
	}
	line, _ := frameTabLayout(sess, false)
	// When main/log TUI is active the active frame should not get brackets.
	if strings.Contains(stripANSI(line), "[aider]") {
		t.Errorf("frameFocused=false: active tab must not have brackets; got %q", stripANSI(line))
	}
}

func TestFrameTabLayoutHitboxesReturnedRegardlessOfFocus(t *testing.T) {
	sess := proto.SessionInfo{
		ID:            "s1",
		ActiveFrameID: "f1",
		Frames: []proto.FrameInfo{
			{ID: "f1", Command: "claude"},
			{ID: "f2", Command: "aider"},
		},
	}
	_, boxesOn := frameTabLayout(sess, true)
	_, boxesOff := frameTabLayout(sess, false)
	if len(boxesOn) != 2 || len(boxesOff) != 2 {
		t.Errorf("expected 2 hitboxes each; got %d/%d", len(boxesOn), len(boxesOff))
	}
}

// TestFrameTabLayoutNotFrameFocusedHitboxWidthsMatchInactiveStyle verifies
// that when frameFocused=false the hitbox widths match the dim (no-bracket)
// render, not the active (bracket) render. Mismatched widths would cause
// click-position drift when tabs are clicked while in main/log mode.
func TestFrameTabLayoutNotFrameFocusedHitboxWidthsMatchInactiveStyle(t *testing.T) {
	sess := proto.SessionInfo{
		ID:            "s1",
		ActiveFrameID: "f1",
		Frames: []proto.FrameInfo{
			{ID: "f1", Command: "claude"},
			{ID: "f2", Command: "aider"},
		},
	}
	_, boxes := frameTabLayout(sess, false)
	for _, h := range boxes {
		var cmd string
		for _, f := range sess.Frames {
			if f.ID == h.frameID {
				cmd = f.Command
			}
		}
		label := frameTabLabel(cmd, 0)
		want := lipgloss.Width(inactiveTabStyle.Render(label))
		got := h.x1 - h.x0
		if got != want {
			t.Errorf("frame %s: hitbox width %d, want %d (inactiveTabStyle width)", h.frameID, got, want)
		}
	}
}
