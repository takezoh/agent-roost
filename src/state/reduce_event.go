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

	next, rawEffs, _, ok := stepDriver(s, FrameID(e.SenderID), DEvHook{
		Event:          e.Event,
		Timestamp:      e.Timestamp,
		RoostSessionID: string(e.SenderID),
		Payload:        e.Payload,
	})
	if !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInternal, "no driver for session")}
	}
	s = next

	// Resolve EffPushDriver effects emitted by the driver.
	var effs []Effect
	for _, eff := range rawEffs {
		pd, isPush := eff.(EffPushDriver)
		if !isPush {
			effs = append(effs, eff)
			continue
		}
		newS, pushEffs, err := pushDriverInternal(s, pd.SessionID, "", pd.Command, LaunchOptions{}, nil, 0, "")
		if err != nil {
			continue
		}
		s = newS
		effs = append(effs, pushEffs...)
	}

	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	if e.ConnID != 0 {
		effs = append(effs, okResp(e.ConnID, e.ReqID, nil))
	}
	return s, effs
}
