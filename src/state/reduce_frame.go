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
	if sess.ActiveFrameID == fid {
		return s, []Effect{okResp(connID, reqID, nil)}
	}
	sess = pushMRU(sess, sess.ActiveFrameID)
	sess.ActiveFrameID = fid
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sid] = sess

	effs := []Effect{
		okResp(connID, reqID, nil),
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
	}
	if s.ActiveSession == sid {
		var pre []Effect
		s, pre = ensureMainAtVisibleSlot(s)
		s.ActiveOccupant = OccupantFrame
		effs = append(effs, pre...)
		effs = append(effs, EffActivateSession{SessionID: sid, Reason: EventActivateFrame})
	}
	return s, effs
}
