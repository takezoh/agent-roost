package state

// ActivateOccupantParams is the payload for EventActivateOccupant.
// Kind must be "main", "log", or "frame" (frame requires SessionID + FrameID).
type ActivateOccupantParams struct {
	Kind      string `json:"kind"`
	SessionID string `json:"session_id,omitempty"`
	FrameID   string `json:"frame_id,omitempty"`
}

func init() {
	RegisterEvent[ActivateOccupantParams](EventActivateOccupant, reduceActivateOccupant)
}

func reduceActivateOccupant(s State, connID ConnID, reqID string, p ActivateOccupantParams) (State, []Effect) {
	switch p.Kind {
	case "log":
		return reduceActivateLog(s, connID, reqID)
	case "main":
		return reduceActivateMain(s, connID, reqID)
	case "frame":
		return reduceActivateOccupantFrame(s, connID, reqID, SessionID(p.SessionID), FrameID(p.FrameID))
	default:
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "unknown occupant kind: "+p.Kind)}
	}
}

// ensureMainAtVisibleSlot swaps pane 0.1 back to the main TUI if the log TUI
// currently occupies it, and clears MainIsLog. Must be called before emitting
// EffActivateSession so the swap source (0.1) is always the main TUI.
func ensureMainAtVisibleSlot(s State) (State, []Effect) {
	if !s.MainIsLog {
		return s, nil
	}
	s.MainIsLog = false
	return s, []Effect{EffSwapHidden{}}
}

func reduceActivateLog(s State, connID ConnID, reqID string) (State, []Effect) {
	var effs []Effect
	if s.ActiveSession != "" {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveSession = ""
	}
	if s.MainIsLog {
		return s, append(effs, okResp(connID, reqID, nil))
	}
	s.MainIsLog = true
	effs = append(effs, EffSwapHidden{}, okResp(connID, reqID, nil))
	return s, effs
}

func reduceActivateMain(s State, connID ConnID, reqID string) (State, []Effect) {
	var effs []Effect
	if s.ActiveSession != "" {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveSession = ""
	}
	if s.MainIsLog {
		s.MainIsLog = false
		effs = append(effs, EffSwapHidden{})
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
	effs = append(effs, EffActivateSession{SessionID: sessID, Reason: EventActivateOccupant})
	effs = append(effs, okResp(connID, reqID, nil))
	return s, effs
}
