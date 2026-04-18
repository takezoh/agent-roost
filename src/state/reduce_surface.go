package state

// SurfaceReadTextReply is the marker passed to EffSendResponseSync.Body for
// surface.read_text. The runtime resolves the pane target from its internal
// sessionPanes map and calls CapturePane to fill in the text.
type SurfaceReadTextReply struct {
	SessionID SessionID
	Lines     int
}

// DriverListReply is the marker passed to EffSendResponseSync.Body for
// driver.list. The runtime builds the response from the driver registry.
type DriverListReply struct{}

func reduceSurfaceReadText(s State, e EvCmdSurfaceReadText) (State, []Effect) {
	sid := e.SessionID
	if sid == "" {
		sid = s.ActiveSession
	}
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{EffSendError{
			ConnID:  e.ConnID,
			ReqID:   e.ReqID,
			Code:    "not_found",
			Message: "session not found: " + string(sid),
		}}
	}
	lines := e.Lines
	if lines <= 0 {
		lines = 30
	}
	return s, []Effect{EffSendResponseSync{
		ConnID: e.ConnID,
		ReqID:  e.ReqID,
		Body:   SurfaceReadTextReply{SessionID: sid, Lines: lines},
	}}
}

func reduceSurfaceSendText(s State, e EvCmdSurfaceSendText) (State, []Effect) {
	sid := e.SessionID
	if sid == "" {
		sid = s.ActiveSession
	}
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{EffSendError{
			ConnID:  e.ConnID,
			ReqID:   e.ReqID,
			Code:    "not_found",
			Message: "session not found: " + string(sid),
		}}
	}
	return s, []Effect{EffSendTmuxKeys{
		ConnID:    e.ConnID,
		ReqID:     e.ReqID,
		SessionID: sid,
		Text:      e.Text,
		WithEnter: true,
	}}
}

func reduceSurfaceSendKey(s State, e EvCmdSurfaceSendKey) (State, []Effect) {
	sid := e.SessionID
	if sid == "" {
		sid = s.ActiveSession
	}
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{EffSendError{
			ConnID:  e.ConnID,
			ReqID:   e.ReqID,
			Code:    "not_found",
			Message: "session not found: " + string(sid),
		}}
	}
	return s, []Effect{EffSendTmuxKeys{
		ConnID:    e.ConnID,
		ReqID:     e.ReqID,
		SessionID: sid,
		Key:       e.Key,
		WithEnter: false,
	}}
}

func reduceDriverList(s State, e EvCmdDriverList) (State, []Effect) {
	return s, []Effect{EffSendResponseSync{
		ConnID: e.ConnID,
		ReqID:  e.ReqID,
		Body:   DriverListReply{},
	}}
}
