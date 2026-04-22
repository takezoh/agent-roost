package state

type statusLineClickPayload struct {
	Range string `json:"range"`
}

func init() {
	RegisterEvent[statusLineClickPayload](EventStatusLineClick, reduceStatusLineClick)
}

func reduceStatusLineClick(s State, connID ConnID, reqID string, p statusLineClickPayload) (State, []Effect) {
	resp := okResp(connID, reqID, nil)
	if p.Range == "" {
		return s, []Effect{resp}
	}
	if s.ActiveOccupant != OccupantFrame || s.ActiveSession == "" {
		return s, []Effect{resp}
	}
	sess, ok := s.Sessions[s.ActiveSession]
	if !ok {
		return s, []Effect{resp}
	}
	frame, ok := activeFrame(sess)
	if !ok {
		return s, []Effect{resp}
	}
	ev := DEvStatusLineClick{
		Range: p.Range,
		Now:   s.Now,
	}
	next, rawEffs, ok := stepDriver(s, frame.ID, ev)
	if !ok {
		return s, []Effect{resp}
	}
	s = next
	s, effs, changed := resolvePushDriverEffects(s, rawEffs)
	if changed {
		effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	}
	return s, append(effs, resp)
}

// resolvePushDriverEffects iterates rawEffs and processes any EffPushDriver
// entries by calling pushDriverInternal. Non-push effects are passed through
// unchanged. Returns the updated State, collected effects, and whether any
// push succeeded (to signal callers that persist/broadcast are needed).
func resolvePushDriverEffects(s State, rawEffs []Effect) (State, []Effect, bool) {
	var effs []Effect
	changed := false
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
		changed = true
		effs = append(effs, pushEffs...)
	}
	return s, effs, changed
}
