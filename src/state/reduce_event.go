package state

func reduceEvent(s State, e EvEvent) (State, []Effect) {
	fn := eventHandlers[e.Event]
	return fn(s, e)
}

func reduceDriverHook(s State, e EvDriverEvent) (State, []Effect) {
	if e.SenderID == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "driver event requires sender_id: "+e.Event)}
	}
	if _, _, _, ok := findFrame(s, FrameID(e.SenderID)); !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "unknown session")}
	}

	next, effs, _, ok := stepDriver(s, FrameID(e.SenderID), DEvHook{
		Event:          e.Event,
		Timestamp:      e.Timestamp,
		RoostSessionID: string(e.SenderID),
		Payload:        e.Payload,
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
