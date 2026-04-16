package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/agent-roost/tui/glyphs"
)

// statusFilter tracks which session statuses are currently visible in the
// sessions list. The array index matches state.Status's iota
// (Running=0..Pending=4). All-true means "show everything" (the default).
type statusFilter [5]bool

// filterStates lists the Status values in chip order, used to translate the
// filter array index back into a state.Status.
var filterStates = [5]state.Status{
	state.StatusRunning,
	state.StatusWaiting,
	state.StatusIdle,
	state.StatusStopped,
	state.StatusPending,
}

func allOnFilter() statusFilter {
	return statusFilter{true, true, true, true, true}
}

func (f statusFilter) matches(s state.Status) bool {
	idx := int(s)
	if idx < 0 || idx >= len(f) {
		return true
	}
	return f[idx]
}

func (f statusFilter) anyOn() bool {
	for _, v := range f {
		if v {
			return true
		}
	}
	return false
}

func (f statusFilter) allOn() bool {
	for _, v := range f {
		if !v {
			return false
		}
	}
	return true
}

// toggle flips the bit for the given state. If the toggle would leave every
// chip off, the filter snaps back to all-on so the list is never empty just
// because of the filter.
func (f *statusFilter) toggle(s state.Status) {
	idx := int(s)
	if idx < 0 || idx >= len(f) {
		return
	}
	f[idx] = !f[idx]
	if !f.anyOn() {
		*f = allOnFilter()
	}
}

// toggleAll flips between all-on and all-off. When every chip is already
// on, it clears the filter so the user can start from a blank slate and
// enable just the chips they want; otherwise it sets every chip on (the
// default "show everything" view).
func (f *statusFilter) toggleAll() {
	if f.allOn() {
		*f = statusFilter{}
		return
	}
	*f = allOnFilter()
}

// chipHitbox records the half-open x range a filter chip occupies on the
// filter bar row, used for mouse hit-testing. isAll marks the trailing All
// reset chip; otherwise state names which status the chip toggles.
type chipHitbox struct {
	state state.Status
	isAll bool
	x0    int
	x1    int
}

// filterBarLayout renders the filter bar and the hitboxes for each chip.
// Pure function: View() and mouse hit-testing call it independently and the
// same filter always yields the same rendered string + hitboxes.
// Each chip is rendered as just the state symbol to keep the bar compact.
func filterBarLayout(f statusFilter) (string, []chipHitbox) {
	var parts []string
	boxes := make([]chipHitbox, 0, len(filterStates)+1)
	x := 0
	for i, st := range filterStates {
		var rendered string
		sym := glyphs.Get(st.SymbolKey())
		if f[i] {
			rendered = filterChipOnStyle.Foreground(stateColor(st)).Render(sym)
		} else {
			rendered = filterChipOffStyle.Render(sym)
		}
		w := lipgloss.Width(rendered)
		boxes = append(boxes, chipHitbox{state: st, x0: x, x1: x + w})
		parts = append(parts, rendered)
		x += w + 1 // chips are joined by a single space
	}

	var allRendered string
	if f.allOn() {
		allRendered = filterAllOnStyle.Render("All")
	} else {
		allRendered = filterAllOffStyle.Render("All")
	}
	allW := lipgloss.Width(allRendered)
	boxes = append(boxes, chipHitbox{isAll: true, x0: x, x1: x + allW})
	parts = append(parts, allRendered)

	return strings.Join(parts, " "), boxes
}

// filterBarRow returns the y coordinate of the filter bar. When the workspace
// bar is visible it sits at row 2 (below title and workspace bar); otherwise
// it moves up to row 1 (below title only).
func (m Model) filterBarRow() int {
	if m.workspaceBarVisible() {
		return 2
	}
	return 1
}

// hitTestFilterChip maps a mouse coordinate to a filter chip. The filter bar
// row depends on whether the workspace bar is currently shown (see filterBarRow).
// Returns hit=false when the click is not on a chip.
func (m Model) hitTestFilterChip(x, y int) (status state.Status, isAll bool, hit bool) {
	if y != m.filterBarRow() {
		return 0, false, false
	}
	_, boxes := filterBarLayout(m.filter)
	for _, b := range boxes {
		if x >= b.x0 && x < b.x1 {
			return b.state, b.isAll, true
		}
	}
	return 0, false, false
}

