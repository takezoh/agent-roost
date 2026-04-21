package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// baseHeaderRows is the header row count when no connectors and no workspace bar
// are present (title + filter bar + blank = 3). Test models have no connectors and
// fewer than 2 workspaces, so the workspace bar is hidden.
const baseHeaderRows = 3

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
		{"cursor unset", []int{2, 2, 2}, -1, 0, 10, 0},
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
	// Row layout (baseHeaderRows=3, workspace bar hidden — fewer than 2 workspaces):
	//   row 0-2: header area (title + filter bar + blank)
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
	// Sticky header is at row 4 (baseHeaderRows=3, +1 for ↑ indicator).
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

func TestTotalItemRows(t *testing.T) {
	m := Model{items: makeItems(2, 3, 1)}
	if got := m.totalItemRows(); got != 6 {
		t.Errorf("totalItemRows = %d, want 6", got)
	}
}

func TestTotalItemRowsEmpty(t *testing.T) {
	m := Model{}
	if got := m.totalItemRows(); got != 0 {
		t.Errorf("totalItemRows = %d, want 0", got)
	}
}

func TestMaxOffset(t *testing.T) {
	tests := []struct {
		name       string
		rows       []int
		bodyHeight int
		wantMax    int
	}{
		{"all fit", []int{2, 2, 2}, 10, 0},
		{"empty", []int{}, 10, 0},
		{"bodyHeight zero", []int{2, 2, 2}, 0, 0},
		// Items: rows=[3,3,3], total=9. bodyHeight=5.
		// At off=2: itemHeight=5-1(↑more)=4, rows[2..]=3 ≤ 4 → fits → continue.
		// At off=1: itemHeight=5-1=4, rows[1..]=6 > 4 → return off+1=2.
		{"overflow two items tail fits", []int{3, 3, 3}, 5, 2},
		// Items: rows=[2,2,2,2], bodyHeight=3.
		// At off=3: itemHeight=3-1=2, rows[3..]=2 ≤ 2 → fits → continue.
		// At off=2: itemHeight=3-1=2, rows[2..]=4 > 2 → return 3.
		{"large list small viewport", []int{2, 2, 2, 2}, 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{items: makeItems(tt.rows...)}
			got := m.maxOffset(tt.bodyHeight)
			if got != tt.wantMax {
				t.Errorf("maxOffset(%d) = %d, want %d", tt.bodyHeight, got, tt.wantMax)
			}
		})
	}
}

func TestMaxOffsetStickyHeader(t *testing.T) {
	// Layout: project "alpha" (1 row) at idx=0, session (2 rows) at idx=1.
	// bodyHeight=4, offset=1 causes sticky header → itemHeight=4-1(↑more)-1(sticky)=2.
	// rows[1..]=2 ≤ 2 → fits. Continue to off=0 (loop skips 0). → return 0.
	items := []listItem{
		{isProject: true, project: "alpha", rows: 1},
		{project: "alpha", rows: 2},
	}
	m := Model{items: items}
	got := m.maxOffset(4)
	if got != 0 {
		t.Errorf("maxOffset with sticky = %d, want 0", got)
	}

	// Same layout but taller session that doesn't fit when sticky header takes a row.
	// At off=1: itemHeight=4-1-1=2, rows[1..]=3 > 2 → return 2.
	items[1].rows = 3
	m.items = items
	got = m.maxOffset(4)
	if got != 2 {
		t.Errorf("maxOffset with sticky overflow = %d, want 2", got)
	}
}

func TestHandleMouseWheelStopsAtMaxOffset(t *testing.T) {
	// 4 items × 3 rows each = 12 total; bodyHeight=5
	// maxOffset(5): at off=3: itemHeight=5-1=4, rows[3..]=3 ≤ 4 → fits.
	//               at off=2: itemHeight=4, rows[2..]=6 > 4 → return 3.
	m := Model{
		items:  makeItems(3, 3, 3, 3),
		height: baseHeaderRows + 5,
		offset: 0,
		cursor: 0,
	}
	bodyHeight := m.height - m.headerRowCount()
	want := m.maxOffset(bodyHeight)

	msg := tea.MouseWheelMsg{Button: tea.MouseWheelDown}
	// Wheel down many times — offset must stop at want.
	for i := 0; i < 10; i++ {
		result, _ := m.handleMouseWheel(msg)
		m = result.(Model)
	}
	if m.offset != want {
		t.Errorf("offset after many WheelDown = %d, want %d (maxOffset)", m.offset, want)
	}
}

func TestHandleMouseWheelNoScrollWhenFits(t *testing.T) {
	m := Model{
		items:  makeItems(2, 2, 2),  // total 6 rows
		height: baseHeaderRows + 10, // bodyHeight=10, fits
		offset: 0,
		cursor: 0,
	}
	msg := tea.MouseWheelMsg{Button: tea.MouseWheelDown}
	result, _ := m.handleMouseWheel(msg)
	got := result.(Model).offset
	if got != 0 {
		t.Errorf("offset = %d, want 0 (should not scroll when content fits)", got)
	}
}

func TestHandleMouseWheelScrollsWhenOverflows(t *testing.T) {
	m := Model{
		items:  makeItems(3, 3, 3), // total 9 rows
		height: baseHeaderRows + 5, // bodyHeight=5, overflows
		offset: 0,
		cursor: 0,
	}
	msg := tea.MouseWheelMsg{Button: tea.MouseWheelDown}
	result, _ := m.handleMouseWheel(msg)
	got := result.(Model).offset
	if got == 0 {
		t.Error("offset should have changed when content overflows")
	}
}

func TestSessionCardLinesNoStateTextOrElapsed(t *testing.T) {
	s := &proto.SessionInfo{
		State: state.StatusRunning,
		View: state.View{
			Card: state.Card{Title: "my-task"},
		},
	}
	lines := sessionCardLines(s, 80, "")
	first := lines[0]
	if strings.Contains(first, "running") {
		t.Errorf("state string leaked into card body: %q", first)
	}
	if strings.Contains(first, "waiting") || strings.Contains(first, "idle") || strings.Contains(first, "stopped") {
		t.Errorf("state string leaked into card body: %q", first)
	}
	// elapsed pattern: one or more digits followed by m/h/d
	for _, line := range lines {
		for _, ch := range []string{"0m", "1m", "0h", "1h", "0d", "1d"} {
			if strings.Contains(line, ch) {
				t.Errorf("elapsed time leaked into card: %q contains %q", line, ch)
			}
		}
	}
	if !strings.Contains(first, "my-task") {
		t.Errorf("title missing from first line: %q", first)
	}
}

func TestSessionCardLinesSubtitleClamp(t *testing.T) {
	mk := func(subtitle string) *proto.SessionInfo {
		// Use Waiting state so the running progress bar does not add an extra line.
		return &proto.SessionInfo{
			State: state.StatusWaiting,
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
			lines := sessionCardLines(mk(tt.subtitle), 80, "")
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

func TestItemCacheKey(t *testing.T) {
	makeSess := func(st state.Status) listItem {
		return listItem{session: &proto.SessionInfo{State: st}}
	}

	// Running and Waiting must not be cached (spinner changes every frame).
	if got := itemCacheKey(makeSess(state.StatusRunning), false, 80, false, "", false); got != "" {
		t.Errorf("Running: want empty key, got %q", got)
	}
	if got := itemCacheKey(makeSess(state.StatusWaiting), false, 80, false, "", false); got != "" {
		t.Errorf("Waiting: want empty key, got %q", got)
	}

	// Idle and Stopped must return a non-empty key.
	idleKey := itemCacheKey(makeSess(state.StatusIdle), false, 80, false, "", false)
	if idleKey == "" {
		t.Error("Idle: want non-empty key")
	}
	stoppedKey := itemCacheKey(makeSess(state.StatusStopped), false, 80, false, "", false)
	if stoppedKey == "" {
		t.Error("Stopped: want non-empty key")
	}

	// Keys must differ when selected/width/notif change.
	k1 := itemCacheKey(makeSess(state.StatusIdle), false, 80, false, "", false)
	k2 := itemCacheKey(makeSess(state.StatusIdle), true, 80, false, "", false)
	if k1 == k2 {
		t.Error("selected=false vs true must produce different keys")
	}
	k3 := itemCacheKey(makeSess(state.StatusIdle), false, 40, false, "", false)
	if k1 == k3 {
		t.Error("width=80 vs 40 must produce different keys")
	}
	k4 := itemCacheKey(makeSess(state.StatusIdle), false, 80, false, "alert", false)
	if k1 == k4 {
		t.Error("notif change must produce different keys")
	}

	// Project items must return non-empty key; fold state must differ.
	proj := listItem{isProject: true, project: "alpha"}
	pk1 := itemCacheKey(proj, false, 80, false, "", false)
	pk2 := itemCacheKey(proj, false, 80, true, "", false)
	if pk1 == "" {
		t.Error("project item: want non-empty key")
	}
	if pk1 == pk2 {
		t.Error("folded vs unfolded must produce different keys")
	}

	// nil session with isProject=false must return empty (guard).
	if got := itemCacheKey(listItem{}, false, 80, false, "", false); got != "" {
		t.Errorf("nil session non-project: want empty key, got %q", got)
	}

	// frameFocused change must bust cache only for multi-frame sessions.
	multiFrameSess := listItem{session: &proto.SessionInfo{
		State:  state.StatusIdle,
		Frames: []proto.FrameInfo{{ID: "f1"}, {ID: "f2"}},
	}}
	mf1 := itemCacheKey(multiFrameSess, false, 80, false, "", true)
	mf2 := itemCacheKey(multiFrameSess, false, 80, false, "", false)
	if mf1 == mf2 {
		t.Error("frameFocused change on multi-frame session must produce different keys")
	}
	singleFrameSess := listItem{session: &proto.SessionInfo{
		State:  state.StatusIdle,
		Frames: []proto.FrameInfo{{ID: "f1"}},
	}}
	sf1 := itemCacheKey(singleFrameSess, false, 80, false, "", true)
	sf2 := itemCacheKey(singleFrameSess, false, 80, false, "", false)
	if sf1 != sf2 {
		t.Error("frameFocused change on single-frame session must NOT change key (no secondary chip)")
	}
}

func TestSessionCardLinesNoFallbackWhenTitleEmpty(t *testing.T) {
	s := &proto.SessionInfo{
		ID:    "abcdef123456",
		State: state.StatusWaiting,
		View: state.View{
			Card: state.Card{Subtitle: "some subtitle"},
		},
	}
	lines := sessionCardLines(s, 80, "")
	for _, line := range lines {
		if strings.Contains(line, "abcdef") {
			t.Errorf("session ID leaked into card when title is empty: %q", line)
		}
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 line (subtitle only), got %d: %v", len(lines), lines)
	}

	// When both title and subtitle are empty, no lines at all.
	empty := &proto.SessionInfo{
		ID:    "abcdef123456",
		State: state.StatusWaiting,
	}
	if got := sessionCardLines(empty, 80, ""); len(got) != 0 {
		t.Errorf("expected 0 lines when title and subtitle both empty, got %d: %v", len(got), got)
	}
}
