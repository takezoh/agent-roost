package state

import (
	"encoding/json"
	"testing"
)

// TestStatusLineClickNoOpWhenRangeEmpty verifies that a click with empty range
// (no named region hit) is ignored regardless of occupant.
func TestStatusLineClickNoOpWhenRangeEmpty(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantFrame
	sessID := SessionID("sess1")
	sess := stubSession(sessID)
	s.Sessions = map[SessionID]Session{sessID: sess}
	s.ActiveSession = sessID
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventStatusLineClick,
		Payload: json.RawMessage(`{"range":""}`),
	})
	mustOK(t, effs)
	if _, ok := findEff[EffSpawnTmuxWindow](effs); ok {
		t.Error("expected no spawn for empty range")
	}
}

// TestStatusLineClickNoOpWhenOccupantMain verifies that a named-range click
// is ignored when the main TUI (not a frame) occupies pane 0.1.
func TestStatusLineClickNoOpWhenOccupantMain(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantMain
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventStatusLineClick,
		Payload: json.RawMessage(`{"range":"plan"}`),
	})
	mustOK(t, effs)
	if _, ok := findEff[EffSpawnTmuxWindow](effs); ok {
		t.Error("expected no spawn for OccupantMain")
	}
}

// TestStatusLineClickNoOpWhenNoSession verifies that a click is ignored
// when OccupantFrame is set but there is no active session.
func TestStatusLineClickNoOpWhenNoSession(t *testing.T) {
	s := New()
	s.ActiveOccupant = OccupantFrame
	s.ActiveSession = ""
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventStatusLineClick,
		Payload: json.RawMessage(`{"range":"plan"}`),
	})
	mustOK(t, effs)
}

// TestStatusLineClickRoutesToDriver verifies that a click is dispatched to
// the active frame's driver when OccupantFrame is set with a valid session.
// The stub driver returns no effects, so only the OK response is emitted.
func TestStatusLineClickRoutesToDriver(t *testing.T) {
	s := New()
	sessID := SessionID("sess1")
	sess := stubSession(sessID)
	s.Sessions = map[SessionID]Session{sessID: sess}
	s.ActiveSession = sessID
	s.ActiveOccupant = OccupantFrame

	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventStatusLineClick,
		Payload: json.RawMessage(`{"range":"plan"}`),
	})
	mustOK(t, effs)
}

// TestStatusLineClickInvalidPayload verifies that a malformed payload
// returns an error rather than panicking.
func TestStatusLineClickInvalidPayload(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvEvent{
		ConnID: 1, ReqID: "r", Event: EventStatusLineClick,
		Payload: json.RawMessage(`{bad json`),
	})
	if _, ok := findEff[EffSendError](effs); !ok {
		t.Error("expected EffSendError for invalid payload")
	}
}
