package tui

import (
	"strings"
	"testing"

	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

func TestSessionCardLinesSubtitleClamp(t *testing.T) {
	mk := func(subtitle string) *proto.SessionInfo {
		return &proto.SessionInfo{
			State: state.StatusRunning,
			View: state.View{
				Card: state.Card{
					Title:    "title",
					Subtitle: subtitle,
				},
			},
		}
	}

	tests := []struct {
		name          string
		subtitle      string
		wantMaxLines  int // subtitle lines (excluding title row)
		wantExactSubs int // exact count of subtitle lines expected
	}{
		{"under limit", "a\nb\nc", 5, 3},
		{"at limit", "a\nb\nc\nd\ne", 5, 5},
		{"over limit clamped", "a\nb\nc\nd\ne\nf\ng", 5, 5},
		{"empty lines skipped", "a\n\nb\n\nc\n\nd\n\ne\n\nf\n\ng", 5, 5},
		{"empty string", "", 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := sessionCardLines(mk(tt.subtitle), 80)
			// First line is always the title row.
			subtitleLines := lines[1:]
			// Filter to only subtitle lines (muted style rendered).
			// Indicators and tags may follow, but with no Tags/Indicators
			// set, everything after the title is subtitle.
			got := len(subtitleLines)
			if got != tt.wantExactSubs {
				t.Errorf("subtitle lines = %d, want %d\nlines: %s",
					got, tt.wantExactSubs, strings.Join(lines, " | "))
			}
			if got > tt.wantMaxLines {
				t.Errorf("subtitle lines %d exceeds max %d", got, tt.wantMaxLines)
			}
		})
	}
}
