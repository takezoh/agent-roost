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

	// Inject reducer-owned values into the payload so the driver can
	// use them without breaking purity (no wall-clock, no session map).
	payload := e.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	needsCopy := false
	if _, hasNow := payload["now"]; !hasNow {
		needsCopy = true
	}
	if _, hasRSID := payload["roost_session_id"]; !hasRSID {
		needsCopy = true
	}
	if needsCopy {
		next := make(map[string]any, len(payload)+2)
		for k, v := range payload {
			next[k] = v
		}
		if _, hasNow := next["now"]; !hasNow {
			next["now"] = s.Now
		}
		if _, hasRSID := next["roost_session_id"]; !hasRSID {
			next["roost_session_id"] = string(e.SessionID)
		}
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
