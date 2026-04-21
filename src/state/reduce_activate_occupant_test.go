package state

import (
	"testing"
)

// TestActivateLogSwapsHidden verifies that activating the log TUI when
// main is visible emits EffSwapHidden and sets ActiveOccupant=log.
func TestActivateLogSwapsHidden(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	if next.ActiveOccupant != OccupantLog {
		t.Errorf("ActiveOccupant = %q, want log", next.ActiveOccupant)
	}
	if _, ok := findEff[EffSwapHidden](effs); !ok {
		t.Error("expected EffSwapHidden")
	}
	mustOK(t, effs)
}

// TestActivateLogIdempotent verifies that calling activate log when log is
// already visible does NOT emit a second swap.
func TestActivateLogIdempotent(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantLog
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	if next.ActiveOccupant != OccupantLog {
		t.Errorf("ActiveOccupant = %q, want log (unchanged)", next.ActiveOccupant)
	}
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden count = %d, want 0 (already at log)", n)
	}
	mustOK(t, effs)
}

// TestActivateMainSwapsHidden verifies that activating main TUI when log is
// visible emits EffSwapHidden and sets ActiveOccupant=main.
func TestActivateMainSwapsHidden(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantLog
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "main"}),
	})
	if next.ActiveOccupant != OccupantMain {
		t.Errorf("ActiveOccupant = %q, want main", next.ActiveOccupant)
	}
	if _, ok := findEff[EffSwapHidden](effs); !ok {
		t.Error("expected EffSwapHidden")
	}
	mustOK(t, effs)
}

// TestActivateMainIdempotent verifies that calling activate main when main is
// already visible does NOT emit a swap.
func TestActivateMainIdempotent(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "main"}),
	})
	if next.ActiveOccupant != OccupantMain {
		t.Errorf("ActiveOccupant = %q, want main (unchanged)", next.ActiveOccupant)
	}
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden count = %d, want 0 (already at main)", n)
	}
	mustOK(t, effs)
}

// TestActivateLogPreservesFocusAndDeactivates verifies that when a session
// frame is active, activating log emits EffDeactivateSession before
// EffSwapHidden and preserves ActiveSession (logical focus).
// Order matters: without deactivate first, the swap source (pane 0.1) would
// be a frame pane instead of the main TUI, corrupting the hidden slot.
func TestActivateLogPreservesFocusAndDeactivates(t *testing.T) {
	s := New()
	s.Sessions["s1"] = Session{
		ID:      "s1",
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
	}
	s.ActiveOccupant = OccupantFrame
	s.ActiveSession = "s1"

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	// Logical focus is preserved across the toggle.
	if next.ActiveSession != "s1" {
		t.Errorf("ActiveSession = %q, want s1 (focus preserved)", next.ActiveSession)
	}
	if next.ActiveOccupant != OccupantLog {
		t.Errorf("ActiveOccupant = %q, want log", next.ActiveOccupant)
	}
	// Order matters: deactivate first (restore 0.1 to main), then swap to log.
	assertEffectOrder[EffDeactivateSession, EffSwapHidden](t, effs)
	mustOK(t, effs)
}

// TestActivateMainPreservesFocus verifies that activate-main also preserves
// ActiveSession when a frame was active.
func TestActivateMainPreservesFocus(t *testing.T) {
	s := New()
	s.Sessions["s1"] = Session{
		ID:      "s1",
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
	}
	s.ActiveOccupant = OccupantFrame
	s.ActiveSession = "s1"

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "main"}),
	})
	if next.ActiveSession != "s1" {
		t.Errorf("ActiveSession = %q, want s1 (focus preserved)", next.ActiveSession)
	}
	if next.ActiveOccupant != OccupantMain {
		t.Errorf("ActiveOccupant = %q, want main", next.ActiveOccupant)
	}
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when frame was active")
	}
	mustOK(t, effs)
}

// TestLogMainRoundTripPreservesFocus verifies that toggling log→main→log keeps
// ActiveSession intact at every step.
func TestLogMainRoundTripPreservesFocus(t *testing.T) {
	s := New()
	s.Sessions["s1"] = Session{
		ID:      "s1",
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
	}
	s.ActiveOccupant = OccupantFrame
	s.ActiveSession = "s1"

	s1, _ := Reduce(s, EvEvent{ConnID: 1, ReqID: "r1", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"})})
	s2, _ := Reduce(s1, EvEvent{ConnID: 1, ReqID: "r2", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "main"})})
	s3, _ := Reduce(s2, EvEvent{ConnID: 1, ReqID: "r3", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"})})

	for i, st := range []State{s1, s2, s3} {
		if st.ActiveSession != "s1" {
			t.Errorf("step %d: ActiveSession = %q, want s1", i+1, st.ActiveSession)
		}
	}
	if s1.ActiveOccupant != OccupantLog {
		t.Errorf("step 1: ActiveOccupant = %q, want log", s1.ActiveOccupant)
	}
	if s2.ActiveOccupant != OccupantMain {
		t.Errorf("step 2: ActiveOccupant = %q, want main", s2.ActiveOccupant)
	}
	if s3.ActiveOccupant != OccupantLog {
		t.Errorf("step 3: ActiveOccupant = %q, want log", s3.ActiveOccupant)
	}
}

// TestActivateOccupantFrameSwapsHiddenWhenLog verifies that switching to a
// frame when log TUI is visible emits EffSwapHidden before EffActivateSession.
func TestActivateOccupantFrameSwapsHiddenWhenLog(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantLog
	frameID := FrameID("frame-1")
	s.Sessions["s1"] = Session{
		ID:            "s1",
		Project:       "/foo",
		Command:       "stub",
		Driver:        stubDriverState{},
		ActiveFrameID: frameID,
		Frames: []SessionFrame{
			{ID: frameID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
		},
	}

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "frame", "session_id": "s1", "frame_id": string(frameID)}),
	})
	if next.ActiveOccupant != OccupantFrame {
		t.Errorf("ActiveOccupant = %q, want frame", next.ActiveOccupant)
	}
	if next.ActiveSession != "s1" {
		t.Errorf("ActiveSession = %q, want s1", next.ActiveSession)
	}
	assertEffectOrder[EffSwapHidden, EffActivateSession](t, effs)
	mustOK(t, effs)
}
