package state

type ActivateFrameParams struct {
	SessionID string `json:"session_id"`
	FrameID   string `json:"frame_id"`
}

func init() {
	RegisterEvent[ActivateFrameParams](EventActivateFrame, reduceActivateFrame)
}

func reduceActivateFrame(s State, connID ConnID, reqID string, p ActivateFrameParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	fid := FrameID(p.FrameID)
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	if findFrameIndex(sess, fid) < 0 {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "frame not found")}
	}

	frameChanged := sess.ActiveFrameID != fid
	needOccupantSwitch := s.ActiveSession == sid && s.ActiveOccupant != OccupantFrame

	if !frameChanged && !needOccupantSwitch {
		return s, []Effect{okResp(connID, reqID, nil)}
	}

	effs := []Effect{okResp(connID, reqID, nil)}

	if frameChanged {
		sess = pushMRU(sess, sess.ActiveFrameID)
		sess.ActiveFrameID = fid
		s.Sessions = cloneSessions(s.Sessions)
		s.Sessions[sid] = sess
		effs = append(effs, EffPersistSnapshot{})
	}
	effs = append(effs, EffBroadcastSessionsChanged{})

	if s.ActiveSession == sid {
		var pre []Effect
		s, pre = ensureMainAtVisibleSlot(s)
		s.ActiveOccupant = OccupantFrame
		effs = append(effs, pre...)
		effs = append(effs,
			EffActivateSession{SessionID: sid, Reason: EventActivateFrame},
			EffSyncStatusLine{Line: ""},
		)
	}
	return s, effs
}
