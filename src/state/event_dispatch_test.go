package state

import (
	"encoding/json"
	"testing"
)

// testFramePayload is a minimal FrameTargeted payload for RegisterFrameEvent tests.
type testFramePayload struct {
	FrameID string `json:"frame_id"`
}

func (p testFramePayload) TargetFrameID() FrameID { return FrameID(p.FrameID) }

// frameDispatchTestState builds a minimal State with one session and one frame.
// The frame uses the "stub" command so driver resolution is deterministic.
func frameDispatchTestState() (State, SessionID, FrameID) {
	s := New()
	sessID := SessionID("sess-dispatch-test")
	frameID := FrameID("frame-dispatch-test")
	s.Sessions[sessID] = Session{
		ID:      sessID,
		Project: "/workspace/dispatch-proj",
		Command: "stub",
		Driver:  stubDriverState{status: StatusIdle},
		Frames: []SessionFrame{{
			ID:      frameID,
			Project: "/workspace/dispatch-proj",
			Command: "stub",
			Driver:  stubDriverState{status: StatusIdle},
		}},
	}
	return s, sessID, frameID
}

// TestRegisterFrameEvent_FrameCtxResolved verifies that when the target frame
// exists, the handler receives a correctly populated FrameCtx.
func TestRegisterFrameEvent_FrameCtxResolved(t *testing.T) {
	const evType = "test.frame_dispatch.resolved"
	defer delete(eventHandlers, evType)

	var capturedCtx FrameCtx
	handlerCalled := false

	RegisterFrameEvent[testFramePayload](evType, func(s State, connID ConnID, reqID string, ctx FrameCtx, p testFramePayload) (State, []Effect) {
		capturedCtx = ctx
		handlerCalled = true
		return s, nil
	})

	st, sessID, frameID := frameDispatchTestState()
	payload, _ := json.Marshal(testFramePayload{FrameID: string(frameID)})

	_, effs := Reduce(st, EvEvent{
		ConnID:  1,
		ReqID:   "r1",
		Event:   evType,
		Payload: json.RawMessage(payload),
	})

	mustOK(t, effs)

	if !handlerCalled {
		t.Fatal("handler was not called")
	}
	if capturedCtx.SessionID != sessID {
		t.Errorf("SessionID = %q, want %q", capturedCtx.SessionID, sessID)
	}
	if capturedCtx.Session.ID != sessID {
		t.Errorf("Session.ID = %q, want %q", capturedCtx.Session.ID, sessID)
	}
	if capturedCtx.Frame.ID != frameID {
		t.Errorf("Frame.ID = %q, want %q", capturedCtx.Frame.ID, frameID)
	}
	if capturedCtx.FrameIndex != 0 {
		t.Errorf("FrameIndex = %d, want 0", capturedCtx.FrameIndex)
	}
	// "stub" driver is registered; Driver must be non-nil and Status must
	// reflect the frame's driver state (StatusIdle set in frameDispatchTestState).
	if capturedCtx.Driver == nil {
		t.Error("Driver = nil, want non-nil (stub driver is registered)")
	}
	if capturedCtx.Status != StatusIdle {
		t.Errorf("Status = %v, want StatusIdle", capturedCtx.Status)
	}
}

// TestRegisterFrameEvent_FrameNotFound verifies that dispatching with an
// unknown FrameID returns ErrCodeNotFound and never calls the handler.
func TestRegisterFrameEvent_FrameNotFound(t *testing.T) {
	const evType = "test.frame_dispatch.notfound"
	defer delete(eventHandlers, evType)

	handlerCalled := false

	RegisterFrameEvent[testFramePayload](evType, func(s State, connID ConnID, reqID string, ctx FrameCtx, p testFramePayload) (State, []Effect) {
		handlerCalled = true
		return s, nil
	})

	st, _, _ := frameDispatchTestState()
	payload, _ := json.Marshal(testFramePayload{FrameID: "ghost-frame-id"})

	_, effs := Reduce(st, EvEvent{
		ConnID:  2,
		ReqID:   "r2",
		Event:   evType,
		Payload: json.RawMessage(payload),
	})

	errEff, ok := findEff[EffSendError](effs)
	if !ok {
		t.Fatal("expected EffSendError for unknown frame")
	}
	if errEff.Code != ErrCodeNotFound {
		t.Errorf("error code = %q, want %q", errEff.Code, ErrCodeNotFound)
	}
	if handlerCalled {
		t.Error("handler must not be called when frame is not found")
	}
}

// TestRegisterFrameEvent_MalformedJSON verifies that a non-JSON payload returns
// ErrCodeInvalidArgument and never calls the handler.
func TestRegisterFrameEvent_MalformedJSON(t *testing.T) {
	const evType = "test.frame_dispatch.malformed"
	defer delete(eventHandlers, evType)

	handlerCalled := false

	RegisterFrameEvent[testFramePayload](evType, func(s State, connID ConnID, reqID string, ctx FrameCtx, p testFramePayload) (State, []Effect) {
		handlerCalled = true
		return s, nil
	})

	st, _, _ := frameDispatchTestState()

	_, effs := Reduce(st, EvEvent{
		ConnID:  3,
		ReqID:   "r3",
		Event:   evType,
		Payload: json.RawMessage([]byte("not-json")),
	})

	errEff, ok := findEff[EffSendError](effs)
	if !ok {
		t.Fatal("expected EffSendError for malformed payload")
	}
	if errEff.Code != ErrCodeInvalidArgument {
		t.Errorf("error code = %q, want %q", errEff.Code, ErrCodeInvalidArgument)
	}
	if handlerCalled {
		t.Error("handler must not be called for malformed payload")
	}
}
