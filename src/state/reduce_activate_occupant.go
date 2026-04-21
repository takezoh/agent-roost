package state

import "github.com/takezoh/agent-roost/uiproc"

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

func reduceActivateLog(s State, connID ConnID, reqID string) (State, []Effect) {
	if s.MainIsLog {
		return s, []Effect{okResp(connID, reqID, nil)}
	}
	var effs []Effect
	if s.ActiveSession != "" {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveSession = ""
	}
	s.MainIsLog = true
	effs = append(effs, EffRespawnMainPane{Proc: uiproc.Log()}, okResp(connID, reqID, nil))
	return s, effs
}

func reduceActivateMain(s State, connID ConnID, reqID string) (State, []Effect) {
	if !s.MainIsLog && s.ActiveSession == "" {
		return s, []Effect{okResp(connID, reqID, nil)}
	}
	var effs []Effect
	if s.ActiveSession != "" {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveSession = ""
	}
	if s.MainIsLog {
		s.MainIsLog = false
		effs = append(effs, EffRespawnMainPane{Proc: uiproc.Main()})
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
	var effs []Effect
	if s.MainIsLog {
		// Swap log out first so 0.1 is back to main before frame swap.
		s.MainIsLog = false
		effs = append(effs, EffRespawnMainPane{Proc: uiproc.Main()})
	}
	effs = append(effs, EffActivateSession{SessionID: sessID, Reason: EventActivateOccupant})
	effs = append(effs, okResp(connID, reqID, nil))
	return s, effs
}
