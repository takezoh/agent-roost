package state

import "encoding/json"

var eventHandlers = map[string]func(State, EvEvent) (State, []Effect){}

func RegisterEvent[T any](eventType string, handler func(State, ConnID, string, T) (State, []Effect)) {
	eventHandlers[eventType] = func(s State, e EvEvent) (State, []Effect) {
		var payload T
		if len(e.Payload) > 0 {
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "invalid payload: "+err.Error())}
			}
		}
		return handler(s, e.ConnID, e.ReqID, payload)
	}
}

// FrameTargeted is implemented by event payloads that address a specific Frame.
type FrameTargeted interface {
	TargetFrameID() FrameID
}

// FrameCtx holds the pre-resolved Frame context injected by RegisterFrameEvent.
// Driver and Status are nil/"" when Frame.Command has no registered driver.
type FrameCtx struct {
	SessionID  SessionID
	Session    Session
	FrameIndex int
	Frame      SessionFrame
	Driver     Driver
	Status     Status
}

// RegisterFrameEvent registers a handler for events whose payload targets a specific Frame.
// The dispatch layer resolves the target Frame before calling handler; if the Frame is not
// found, ErrCodeNotFound is returned automatically.
func RegisterFrameEvent[T FrameTargeted](eventType string, handler func(State, ConnID, string, FrameCtx, T) (State, []Effect)) {
	eventHandlers[eventType] = func(s State, e EvEvent) (State, []Effect) {
		var payload T
		if len(e.Payload) > 0 {
			if err := json.Unmarshal(e.Payload, &payload); err != nil {
				return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "invalid payload: "+err.Error())}
			}
		}
		frameID := payload.TargetFrameID()
		sessID, sess, idx, ok := findFrame(s, frameID)
		if !ok {
			return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "frame not found")}
		}
		frame := sess.Frames[idx]
		drv := GetDriver(frame.Command)
		var status Status
		if drv != nil {
			status = drv.Status(frame.Driver)
		}
		ctx := FrameCtx{
			SessionID:  sessID,
			Session:    sess,
			FrameIndex: idx,
			Frame:      frame,
			Driver:     drv,
			Status:     status,
		}
		return handler(s, e.ConnID, e.ReqID, ctx, payload)
	}
}

func IsRegisteredEvent(name string) bool {
	_, ok := eventHandlers[name]
	return ok
}
