package state

// Hook event reducer. Routes the typed hook payload to the right
// session's driver via stepDriver, then appends the standard
// post-driver effects (persist + broadcast) and the response.

func reduceHook(s State, e EvCmdHook) (State, []Effect) {
	if e.SessionID == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "session_id required")}
	}
	if _, ok := s.Sessions[e.SessionID]; !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "unknown session")}
	}

	// Inject the reducer's clock into the payload so the driver can
	// stamp transitions without reading wall-clock from inside Step.
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if _, hasNow := payload["now"]; !hasNow {
		// Avoid mutating the caller's map.
		next := make(map[string]any, len(payload)+1)
		for k, v := range payload {
			next[k] = v
		}
		next["now"] = s.Now
		payload = next
	}

	next, effs, _, ok := stepDriver(s, e.SessionID, DEvHook{
		Event:   e.Event,
		Payload: payload,
	})
	if !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInternal, "no driver for session")}
	}
	s = next

	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	if e.ConnID != 0 {
		effs = append(effs, okResp(e.ConnID, e.ReqID, nil))
	}
	return s, effs
}
