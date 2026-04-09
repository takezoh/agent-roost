package tui

import (
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/state"
)

func TestStatusFilterMatches(t *testing.T) {
	f := statusFilter{true, false, true, false, true}
	cases := []struct {
		state state.Status
		want  bool
	}{
		{state.StatusRunning, true},
		{state.StatusWaiting, false},
		{state.StatusIdle, true},
		{state.StatusStopped, false},
		{state.StatusPending, true},
	}
	for _, c := range cases {
		if got := f.matches(c.state); got != c.want {
			t.Errorf("matches(%s) = %v, want %v", c.state, got, c.want)
		}
	}
}

func TestStatusFilterToggleFlipsBit(t *testing.T) {
	f := allOnFilter()
	f.toggle(state.StatusIdle)
	if f.matches(state.StatusIdle) {
		t.Fatal("idle should be off after first toggle")
	}
	f.toggle(state.StatusIdle)
	if !f.matches(state.StatusIdle) {
		t.Fatal("idle should be on after second toggle")
	}
}

func TestStatusFilterToggleAllOffResetsToAllOn(t *testing.T) {
	// Only running is on; toggling it off should snap the filter back to
	// all-on rather than producing an empty list.
	f := statusFilter{true, false, false, false, false}
	f.toggle(state.StatusRunning)
	if !f.allOn() {
		t.Fatalf("expected filter to reset to all-on, got %#v", f)
	}
}

func TestStatusFilterToggleAll(t *testing.T) {
	// All-on → all-off: lets the user clear the filter and enable just the
	// chips they want with a single click.
	f := allOnFilter()
	f.toggleAll()
	if f.anyOn() {
		t.Fatalf("expected all-off after toggleAll from all-on, got %#v", f)
	}

	// All-off → all-on.
	f.toggleAll()
	if !f.allOn() {
		t.Fatalf("expected all-on after toggleAll from all-off, got %#v", f)
	}

	// Partial → all-on.
	f = statusFilter{true, false, true, false, false}
	f.toggleAll()
	if !f.allOn() {
		t.Fatalf("expected all-on after toggleAll from partial, got %#v", f)
	}
}

func TestFilterBarLayoutHitboxesAlignWithRender(t *testing.T) {
	f := allOnFilter()
	rendered, boxes := filterBarLayout(f)

	if len(boxes) != 6 {
		t.Fatalf("expected 6 hitboxes (5 chips + All), got %d", len(boxes))
	}

	// The last hitbox must end at exactly the rendered width — that's the
	// invariant that keeps mouse hit-testing in sync with what's drawn.
	if got, want := boxes[len(boxes)-1].x1, lipgloss.Width(rendered); got != want {
		t.Errorf("last hitbox x1 = %d, rendered width = %d", got, want)
	}

	// Hitboxes must be in order, non-overlapping (chips are joined by a
	// single space, so each box[i+1].x0 should be box[i].x1 + 1).
	for i := 1; i < len(boxes); i++ {
		if boxes[i].x0 != boxes[i-1].x1+1 {
			t.Errorf("hitbox %d gap mismatch: prev.x1=%d, curr.x0=%d",
				i, boxes[i-1].x1, boxes[i].x0)
		}
	}

	// First five boxes correspond to the five Statuses in iota order.
	for i, st := range filterStates {
		if boxes[i].state != st {
			t.Errorf("box %d state = %v, want %v", i, boxes[i].state, st)
		}
		if boxes[i].isAll {
			t.Errorf("box %d should not be the All chip", i)
		}
	}
	if !boxes[5].isAll {
		t.Error("last box should be the All chip")
	}
}

func TestHitTestFilterChip(t *testing.T) {
	m := NewModel(nil, &config.Config{})
	m.sessions = []core.SessionInfo{
		{State: state.StatusRunning},
		{State: state.StatusWaiting},
	}
	_, boxes := filterBarLayout(m.filter)

	// Click in the middle of the first chip (running) — should hit it.
	x := (boxes[0].x0 + boxes[0].x1) / 2
	status, isAll, hit := m.hitTestFilterChip(x, 1)
	if !hit || isAll || status != state.StatusRunning {
		t.Errorf("expected hit on running, got hit=%v isAll=%v status=%v", hit, isAll, status)
	}

	// Click on the All chip.
	allBox := boxes[len(boxes)-1]
	x = (allBox.x0 + allBox.x1) / 2
	_, isAll, hit = m.hitTestFilterChip(x, 1)
	if !hit || !isAll {
		t.Errorf("expected All hit, got hit=%v isAll=%v", hit, isAll)
	}

	// Click on the wrong row should miss.
	if _, _, hit := m.hitTestFilterChip(x, 0); hit {
		t.Error("y=0 should not hit any chip")
	}
	if _, _, hit := m.hitTestFilterChip(x, 5); hit {
		t.Error("y=5 should not hit any chip")
	}
}
