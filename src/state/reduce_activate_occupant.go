package state

// ActivateOccupantParams is the payload for EventActivateOccupant.
// Kind must be "main", "log", or "frame" (frame requires SessionID + FrameID).
type ActivateOccupantParams struct {
	Kind      OccupantKind `json:"kind"`
	SessionID string       `json:"session_id,omitempty"`
	FrameID   string       `json:"frame_id,omitempty"`
}

func init() {
	RegisterEvent[ActivateOccupantParams](EventActivateOccupant, reduceActivateOccupant)
}

func reduceActivateOccupant(s State, connID ConnID, reqID string, p ActivateOccupantParams) (State, []Effect) {
	switch p.Kind {
	case OccupantLog, OccupantMain:
		return reduceActivateOccupantSlot(s, connID, reqID, p.Kind)
	case OccupantFrame:
		return reduceActivateOccupantFrame(s, connID, reqID, SessionID(p.SessionID), FrameID(p.FrameID))
	default:
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "unknown occupant kind: "+string(p.Kind))}
	}
}

// ensureMainAtVisibleSlot swaps pane 0.1 back to the main TUI if the log TUI
// currently occupies it. Must be called before emitting EffActivateSession so
// the swap source (0.1) is always the main TUI.
func ensureMainAtVisibleSlot(s State) (State, []Effect) {
	if s.ActiveOccupant != OccupantLog {
		return s, nil
	}
	s.ActiveOccupant = OccupantMain
	return s, []Effect{EffSwapHidden{}}
}

// reduceActivateOccupantSlot handles activate-main and activate-log.
// ActiveSession (logical focus) is preserved across both transitions. A
// sessions-changed broadcast is emitted so the header tab bar and sessions
// sidebar can re-render with the new occupant-derived styling (frame tabs
// and secondary chips dim when the slot no longer holds a frame).
func reduceActivateOccupantSlot(s State, connID ConnID, reqID string, target OccupantKind) (State, []Effect) {
	var effs []Effect
	changed := false
	if s.ActiveOccupant == OccupantFrame {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveOccupant = OccupantMain
		changed = true
	}
	if s.ActiveOccupant != target {
		s.ActiveOccupant = target
		effs = append(effs, EffSwapHidden{})
		changed = true
	}
	if changed {
		effs = append(effs, EffBroadcastSessionsChanged{})
	}
	effs = append(effs, okResp(connID, reqID, nil))
	return s, effs
}

func reduceActivateOccupantFrame(s State, connID ConnID, reqID string, sessID SessionID, frameID FrameID) (State, []Effect) {
	sess, ok := s.Sessions[sessID]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	if findFrameIndex(sess, frameID) < 0 {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "frame not found")}
	}
	s, effs := ensureMainAtVisibleSlot(s)
	s.ActiveOccupant = OccupantFrame
	s.ActiveSession = sessID
	effs = append(effs,
		EffActivateSession{SessionID: sessID, Reason: EventActivateOccupant},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, nil),
	)
	return s, effs
}
