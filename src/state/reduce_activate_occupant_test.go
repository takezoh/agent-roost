package state

import (
	"testing"
)

// TestActivateLogSwapsHidden verifies that activating the log TUI when
// main is visible emits EffSwapHidden and sets MainIsLog=true.
func TestActivateLogSwapsHidden(t *testing.T) {
	s := New()
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	if !next.MainIsLog {
		t.Error("MainIsLog should be true after activate log")
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
	s.MainIsLog = true
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	if !next.MainIsLog {
		t.Error("MainIsLog should stay true")
	}
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden count = %d, want 0 (already at log)", n)
	}
	mustOK(t, effs)
}

// TestActivateMainSwapsHidden verifies that activating main TUI when log is
// visible emits EffSwapHidden and clears MainIsLog.
func TestActivateMainSwapsHidden(t *testing.T) {
	s := New()
	s.MainIsLog = true
	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "main"}),
	})
	if next.MainIsLog {
		t.Error("MainIsLog should be false after activate main")
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
	if next.MainIsLog {
		t.Error("MainIsLog should remain false")
	}
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden count = %d, want 0 (already at main)", n)
	}
	mustOK(t, effs)
}

// TestActivateLogDeactivatesActiveSession verifies that when a session is
// active, activating log emits EffDeactivateSession before EffSwapHidden.
// Order matters: without deactivate first, the swap source (pane 0.1) would
// be a frame pane instead of the main TUI, corrupting the hidden slot.
func TestActivateLogDeactivatesActiveSession(t *testing.T) {
	s := New()
	s.Sessions["s1"] = Session{
		ID:      "s1",
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
	}
	s.ActiveSession = "s1"

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-occupant",
		Payload: mustPayload(map[string]string{"kind": "log"}),
	})
	if next.ActiveSession != "" {
		t.Errorf("ActiveSession = %q, want empty", next.ActiveSession)
	}
	if !next.MainIsLog {
		t.Error("MainIsLog should be true")
	}
	assertEffectOrder[EffDeactivateSession, EffSwapHidden](t, effs)
	mustOK(t, effs)
}

// TestActivateOccupantFrameSwapsHiddenWhenLog verifies that switching to a
// frame when log TUI is visible emits EffSwapHidden before EffActivateSession.
func TestActivateOccupantFrameSwapsHiddenWhenLog(t *testing.T) {
	s := New()
	s.MainIsLog = true
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
	if next.MainIsLog {
		t.Error("MainIsLog should be false after frame activation")
	}
	assertEffectOrder[EffSwapHidden, EffActivateSession](t, effs)
	mustOK(t, effs)
}
