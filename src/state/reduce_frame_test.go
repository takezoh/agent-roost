package state

import (
	"testing"
)

// makeSessionWithFrames creates a state with one session that has two frames:
// root (f1) and a child (f2), with f1 as the active frame.
func makeSessionWithFrames() State {
	s := New()
	s.Sessions["s1"] = Session{
		ID:            "s1",
		Project:       "/foo",
		Command:       "stub",
		Driver:        stubDriverState{},
		ActiveFrameID: "f1",
		Frames: []SessionFrame{
			{ID: "f1", Project: "/foo", Command: "stub", Driver: stubDriverState{}},
			{ID: "f2", Project: "/foo", Command: "alt", Driver: stubDriverState{}},
		},
	}
	s.ActiveSession = "s1"
	return s
}

// TestActivateFrameIdempotentWhenAlreadyFrameAndSame verifies that clicking the
// already-active frame while occupant=frame is a complete no-op: only okResp.
func TestActivateFrameIdempotentWhenAlreadyFrameAndSame(t *testing.T) {
	s := makeSessionWithFrames()
	s.ActiveOccupant = OccupantFrame

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-frame",
		Payload: mustPayload(map[string]string{"session_id": "s1", "frame_id": "f1"}),
	})
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden = %d, want 0", n)
	}
	if n := countEff[EffPersistSnapshot](effs); n != 0 {
		t.Errorf("EffPersistSnapshot = %d, want 0", n)
	}
	if n := countEff[EffBroadcastSessionsChanged](effs); n != 0 {
		t.Errorf("EffBroadcastSessionsChanged = %d, want 0", n)
	}
	if n := countEff[EffActivateSession](effs); n != 0 {
		t.Errorf("EffActivateSession = %d, want 0", n)
	}
	mustOK(t, effs)
}

// TestActivateSameFrameFromLogSwapsToFrame verifies that clicking the
// already-active frame while occupant=log triggers occupant switch
// WITHOUT updating frame state (no persist, no MRU change).
func TestActivateSameFrameFromLogSwapsToFrame(t *testing.T) {
	s := makeSessionWithFrames()
	s.ActiveOccupant = OccupantLog

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-frame",
		Payload: mustPayload(map[string]string{"session_id": "s1", "frame_id": "f1"}),
	})
	if next.ActiveOccupant != OccupantFrame {
		t.Errorf("ActiveOccupant = %q, want frame", next.ActiveOccupant)
	}
	if sess := next.Sessions["s1"]; sess.ActiveFrameID != "f1" {
		t.Errorf("ActiveFrameID = %q, want f1 (unchanged)", sess.ActiveFrameID)
	}
	if _, ok := findEff[EffSwapHidden](effs); !ok {
		t.Error("expected EffSwapHidden for log→main swap")
	}
	if _, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged")
	}
	if n := countEff[EffPersistSnapshot](effs); n != 0 {
		t.Errorf("EffPersistSnapshot = %d, want 0 (frame unchanged)", n)
	}
	mustOK(t, effs)
}

// TestActivateDifferentFrameFromLogSwapsAndChangesFrame verifies that clicking
// a different frame tab while occupant=log does both: swap log→frame AND
// update ActiveFrameID + persist.
func TestActivateDifferentFrameFromLogSwapsAndChangesFrame(t *testing.T) {
	s := makeSessionWithFrames()
	s.ActiveOccupant = OccupantLog

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-frame",
		Payload: mustPayload(map[string]string{"session_id": "s1", "frame_id": "f2"}),
	})
	if next.ActiveOccupant != OccupantFrame {
		t.Errorf("ActiveOccupant = %q, want frame", next.ActiveOccupant)
	}
	if sess := next.Sessions["s1"]; sess.ActiveFrameID != "f2" {
		t.Errorf("ActiveFrameID = %q, want f2", sess.ActiveFrameID)
	}
	if _, ok := findEff[EffSwapHidden](effs); !ok {
		t.Error("expected EffSwapHidden")
	}
	if _, ok := findEff[EffPersistSnapshot](effs); !ok {
		t.Error("expected EffPersistSnapshot")
	}
	if _, ok := findEff[EffBroadcastSessionsChanged](effs); !ok {
		t.Error("expected EffBroadcastSessionsChanged")
	}
	if _, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	}
	mustOK(t, effs)
}

// TestActivateFrameFromMainDoesNotSwap verifies that activating a frame while
// occupant=main does NOT emit EffSwapHidden (ensureMainAtVisibleSlot no-ops
// when already main), but does emit EffActivateSession.
func TestActivateFrameFromMainDoesNotSwap(t *testing.T) {
	s := makeSessionWithFrames()
	s.ActiveOccupant = OccupantMain

	next, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: "activate-frame",
		Payload: mustPayload(map[string]string{"session_id": "s1", "frame_id": "f2"}),
	})
	if next.ActiveOccupant != OccupantFrame {
		t.Errorf("ActiveOccupant = %q, want frame", next.ActiveOccupant)
	}
	if n := countEff[EffSwapHidden](effs); n != 0 {
		t.Errorf("EffSwapHidden = %d, want 0 (already at main)", n)
	}
	if _, ok := findEff[EffActivateSession](effs); !ok {
		t.Error("expected EffActivateSession")
	}
	mustOK(t, effs)
}
