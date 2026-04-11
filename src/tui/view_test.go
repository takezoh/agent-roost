package tui

import (
	"strings"
	"testing"

	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

func makeItems(rowCounts ...int) []listItem {
	items := make([]listItem, len(rowCounts))
	for i, r := range rowCounts {
		items[i] = listItem{rows: r}
	}
	return items
}

func TestEnsureCursorVisible(t *testing.T) {
	tests := []struct {
		name       string
		rows       []int
		cursor     int
		offset     int
		bodyHeight int
		wantOffset int
	}{
		{"all fit", []int{2, 2, 2}, 2, 0, 10, 0},
		{"cursor below viewport", []int{3, 3, 3}, 2, 0, 7, 1},
		{"cursor above offset", []int{2, 2, 2}, 0, 2, 10, 0},
		{"single tall item", []int{10}, 0, 0, 5, 0},
		{"scroll to last", []int{2, 2, 2, 2}, 3, 0, 3, 3},
		{"already visible", []int{2, 2, 2}, 1, 0, 6, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				items:  makeItems(tt.rows...),
				cursor: tt.cursor,
				offset: tt.offset,
			}
			m.ensureCursorVisible(tt.bodyHeight)
			if m.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", m.offset, tt.wantOffset)
			}
		})
	}
}

func TestVisibleEnd(t *testing.T) {
	tests := []struct {
		name       string
		rows       []int
		offset     int
		bodyHeight int
		wantEnd    int
	}{
		{"all fit", []int{2, 2, 2}, 0, 10, 3},
		{"partial", []int{3, 3, 3}, 0, 5, 1},
		{"from offset", []int{3, 3, 3}, 1, 7, 3},
		{"exact fit", []int{2, 2, 2}, 0, 8, 3}, // 2+2+2=6 <= 8
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{
				items:  makeItems(tt.rows...),
				offset: tt.offset,
			}
			got := m.visibleEnd(tt.bodyHeight)
			if got != tt.wantEnd {
				t.Errorf("visibleEnd = %d, want %d", got, tt.wantEnd)
			}
		})
	}
}

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
