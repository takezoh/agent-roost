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

func TestStickyProject(t *testing.T) {
	items := []listItem{
		{isProject: true, project: "alpha"},
		{project: "alpha"},
		{project: "alpha"},
		{isProject: true, project: "beta"},
		{project: "beta"},
	}

	tests := []struct {
		name   string
		offset int
		want   string
	}{
		{"offset 0, no sticky", 0, ""},
		{"offset on project header", 3, ""},
		{"offset past alpha header", 1, "alpha"},
		{"offset on last alpha session", 2, "alpha"},
		{"offset past beta header", 4, "beta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stickyProject(items, tt.offset)
			if got != tt.want {
				t.Errorf("stickyProject(offset=%d) = %q, want %q", tt.offset, got, tt.want)
			}
		})
	}
}

func TestRowToItemIndexWithStickyHeader(t *testing.T) {
	// Layout: project "alpha" (header idx=0), session (idx=1), session (idx=2)
	items := []listItem{
		{isProject: true, project: "alpha", rows: 1},
		{project: "alpha", rows: 3},
		{project: "alpha", rows: 3},
	}
	m := Model{
		items:  items,
		offset: 1, // project header scrolled out → sticky header shown
	}
	// Row layout (sessionsHeaderRows=3):
	//   row 0-2: header area
	//   row 3:   "↑ N more"
	//   row 4:   sticky "alpha" header
	//   row 5-7: session idx=1 (3 rows)
	//   row 8-10: session idx=2 (3 rows)

	// Click on sticky header → returns project header index (0)
	if got := m.rowToItemIndex(4); got != 0 {
		t.Errorf("sticky header click: got %d, want 0", got)
	}
	// Click on first visible session
	if got := m.rowToItemIndex(5); got != 1 {
		t.Errorf("first session click: got %d, want 1", got)
	}
	// Click on second session
	if got := m.rowToItemIndex(8); got != 2 {
		t.Errorf("second session click: got %d, want 2", got)
	}
}

func TestRowToItemIndexStickyHeaderNoHoverJump(t *testing.T) {
	items := []listItem{
		{isProject: true, project: "alpha", rows: 1},
		{project: "alpha", rows: 3},
		{project: "alpha", rows: 3},
	}
	m := Model{
		items:  items,
		offset: 1,
	}
	// Sticky header is at row 4 (sessionsHeaderRows=3, +1 for ↑ indicator).
	// rowToItemIndex returns 0 (project header index), which is < m.offset.
	idx := m.rowToItemIndex(4)
	if idx != 0 {
		t.Fatalf("expected project header index 0, got %d", idx)
	}
	// handleMouseMotion should skip cursor update because idx < m.offset.
	if idx >= m.offset {
		t.Error("sticky header index should be less than offset")
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
