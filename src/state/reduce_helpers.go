package state

import (
	"crypto/rand"
	"encoding/hex"
)

// Reducer helpers shared by reduce_*.go files. These are pure
// functions that operate on State values; they may allocate new
// SessionIDs / JobIDs and post-process the side-effect lists driver
// Step methods return.

// allocSessionID generates a fresh, random session id. crypto/rand is
// the same source the legacy session.SessionService used so old and
// new ids are interchangeable in the on-disk snapshot. The 12-byte
// width matches the legacy format.
func allocSessionID() SessionID {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure on Linux is effectively impossible (it
		// reads from /dev/urandom). If it ever happens we want to fail
		// loud rather than emit a deterministic id that could collide.
		panic("state: crypto/rand failed: " + err.Error())
	}
	return SessionID(hex.EncodeToString(b[:]))
}

func allocFrameID() FrameID {
	return FrameID(allocSessionID())
}

func rootFrame(sess Session) (SessionFrame, bool) {
	if len(sess.Frames) == 0 {
		if sess.Command == "" || sess.Driver == nil {
			return SessionFrame{}, false
		}
		return SessionFrame{
			ID:            FrameID(sess.ID),
			Project:       sess.Project,
			Command:       sess.Command,
			LaunchOptions: sess.LaunchOptions,
			CreatedAt:     sess.CreatedAt,
			Driver:        sess.Driver,
		}, true
	}
	return sess.Frames[0], true
}

func activeFrame(sess Session) (SessionFrame, bool) {
	if len(sess.Frames) == 0 {
		return rootFrame(sess)
	}
	return sess.Frames[len(sess.Frames)-1], true
}

func findFrameIndex(sess Session, frameID FrameID) int {
	for i, frame := range sess.Frames {
		if frame.ID == frameID {
			return i
		}
	}
	return -1
}

func findFrame(s State, frameID FrameID) (SessionID, Session, int, bool) {
	for sessID, sess := range s.Sessions {
		if idx := findFrameIndex(sess, frameID); idx >= 0 {
			return sessID, sess, idx, true
		}
	}
	return "", Session{}, -1, false
}

func truncateFrames(sess Session, from int) (Session, []SessionFrame) {
	if from < 0 || from >= len(sess.Frames) {
		return sess, nil
	}
	removed := append([]SessionFrame(nil), sess.Frames[from:]...)
	sess.Frames = append([]SessionFrame(nil), sess.Frames[:from]...)
	return sess, removed
}

// stepDriver runs the per-session driver Step inside the reducer and
// post-processes the returned effects so callers don't have to clone
// the State map themselves. Returns the new State (with the updated
// session and any newly registered jobs), the post-processed effects,
// and a "found" bool that's false when the session id is unknown.
//
// Effect post-processing:
//   - EffStartJob: assigns a fresh JobID, records JobMeta with the
//     owning session id and kind, and rewrites the effect to carry
//     the new id.
//   - EffEventLogAppend / EffWatchFile / EffUnwatchFile:
//     fills in the SessionID field if the driver left it blank.
func stepDriver(s State, frameID FrameID, ev DriverEvent) (State, []Effect, View, bool) {
	sessID, sess, frameIdx, ok := findFrame(s, frameID)
	if !ok {
		return s, nil, View{}, false
	}
	frame := sess.Frames[frameIdx]
	drv := GetDriver(frame.Command)
	if drv == nil {
		return s, nil, View{}, false
	}
	nextDS, rawEffs, view := drv.Step(frame.Driver, ev)

	s.Sessions = cloneSessions(s.Sessions)
	sess.Frames = append([]SessionFrame(nil), sess.Frames...)
	frame.Driver = nextDS
	sess.Frames[frameIdx] = frame
	s.Sessions[sessID] = sess

	if len(rawEffs) == 0 {
		return s, nil, view, true
	}

	out := make([]Effect, 0, len(rawEffs))
	for _, eff := range rawEffs {
		patched, newState := postProcessEffect(s, sessID, frameID, eff)
		s = newState
		out = append(out, patched)
	}
	return s, out, view, true
}

// postProcessEffect fills in session-context fields the driver Step
// left blank and (for EffStartJob) registers JobMeta + assigns a fresh
// JobID. Returns the patched effect and the (possibly mutated) State.
func postProcessEffect(s State, sessID SessionID, frameID FrameID, eff Effect) (Effect, State) {
	switch e := eff.(type) {
	case EffStartJob:
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{
			SessionID: sessID,
			FrameID:   frameID,
			StartedAt: s.Now,
		}
		e.JobID = jobID
		return e, s
	case EffEventLogAppend:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e, s
	case EffWatchFile:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e, s
	case EffUnwatchFile:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e, s
	case EffPushDriver:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e, s
	default:
		return eff, s
	}
}

// stepActiveSessions runs Step against sessions that need ticking.
// Idle and Stopped sessions are skipped (hook events will wake them).
// Returns whether any session emitted effects, so the caller can
// decide whether to broadcast/persist.
func stepActiveSessions(s State, makeEv func(sessID SessionID, sess Session, active bool) DriverEvent) (State, []Effect, bool) {
	if len(s.Sessions) == 0 {
		return s, nil, false
	}
	// Sort session IDs for deterministic effect ordering.
	ids := make([]SessionID, 0, len(s.Sessions))
	for id := range s.Sessions {
		ids = append(ids, id)
	}
	sortSessionIDs(ids)
	var effs []Effect
	changed := false
	for _, sessID := range ids {
		sess := s.Sessions[sessID]
		frame, ok := activeFrame(sess)
		if !ok {
			continue
		}
		drv := GetDriver(frame.Command)
		if drv == nil {
			continue
		}
		status := drv.Status(frame.Driver)
		if status == StatusIdle || status == StatusStopped {
			continue
		}
		active := sessID == s.ActiveSession
		ev := makeEv(sessID, sess, active)
		next, sessEffs, _, ok := stepDriver(s, frame.ID, ev)
		if !ok {
			continue
		}
		s = next
		if len(sessEffs) > 0 {
			changed = true
			effs = append(effs, sessEffs...)
		}
	}
	return s, effs, changed
}

func sortSessionIDs(ids []SessionID) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// errResp wraps a typed error code + message into an EffSendError
// effect. Reducers use this to keep error reply construction terse.
func errResp(connID ConnID, reqID, code, message string) Effect {
	return EffSendError{
		ConnID:  connID,
		ReqID:   reqID,
		Code:    code,
		Message: message,
	}
}

// okResp wraps a typed success body into an EffSendResponse.
func okResp(connID ConnID, reqID string, body any) Effect {
	return EffSendResponse{
		ConnID: connID,
		ReqID:  reqID,
		Body:   body,
	}
}

// stepConnector runs a connector's Step and post-processes effects.
// Mirrors stepDriver but for daemon-level connectors rather than
// per-session drivers. Returns the new State, post-processed effects,
// and a "found" bool that's false when the connector is unknown.
func stepConnector(s State, name string, ev ConnectorEvent) (State, []Effect, bool) {
	conn := GetConnector(name)
	if conn == nil {
		return s, nil, false
	}
	cs := s.Connectors[name]
	if cs == nil {
		return s, nil, false
	}
	nextCS, rawEffs := conn.Step(cs, ev)

	s.Connectors = cloneConnectors(s.Connectors)
	s.Connectors[name] = nextCS

	if len(rawEffs) == 0 {
		return s, nil, true
	}

	out := make([]Effect, 0, len(rawEffs))
	for _, eff := range rawEffs {
		patched, newState := postProcessConnectorEffect(s, name, eff)
		s = newState
		out = append(out, patched)
	}
	return s, out, true
}

// postProcessConnectorEffect fills in connector context for effects.
// For EffStartJob, it allocates a JobID and records JobMeta with
// Connector set instead of SessionID.
func postProcessConnectorEffect(s State, connName string, eff Effect) (Effect, State) {
	switch e := eff.(type) {
	case EffStartJob:
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{
			Connector: connName,
			StartedAt: s.Now,
		}
		e.JobID = jobID
		return e, s
	default:
		return eff, s
	}
}

// evictFrame removes all frames from frameID onward, updates state, and
// returns the effects to clean up tmux and file-watch resources.
//
// killWindow controls whether EffKillSessionWindow is emitted per removed
// frame. Pass true when the tmux window still exists (e.g. EvPaneDied);
// pass false when the window has already vanished (e.g. EvTmuxWindowVanished).
//
// Effect ordering: deactivate → reactivate → cleanup → persist → broadcast.
// Reactivate precedes cleanup so that EffActivateSession swaps the parent
// pane into 0.0 before EffKillSessionWindow destroys the old window —
// preventing kill-window from targeting window 0.
func evictFrame(s State, frameID FrameID, killWindow bool) (State, []Effect, bool) {
	sessID, sess, idx, ok := findFrame(s, frameID)
	if !ok {
		return s, nil, false
	}
	nextSess, removed := truncateFrames(sess, idx)
	s.Sessions = cloneSessions(s.Sessions)
	if len(nextSess.Frames) == 0 {
		delete(s.Sessions, sessID)
	} else {
		s.Sessions[sessID] = nextSess
	}
	var deactivate []Effect
	var reactivate []Effect
	if s.ActiveSession == sessID && len(nextSess.Frames) == 0 {
		s.ActiveSession = ""
		deactivate = []Effect{EffDeactivateSession{}}
	} else if s.ActiveSession == sessID {
		reactivate = []Effect{EffActivateSession{SessionID: sessID, Reason: EventSwitchSession}}
	}
	var cleanup []Effect
	for _, frame := range removed {
		if killWindow {
			cleanup = append(cleanup, EffKillSessionWindow{FrameID: frame.ID})
		}
		cleanup = append(cleanup,
			EffUnregisterPane{FrameID: frame.ID},
			EffUnwatchFile{FrameID: frame.ID},
		)
	}
	effs := append(deactivate, reactivate...)
	effs = append(effs, cleanup...)
	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs, true
}

// === ErrCode constants used by reducers ===
//
// These mirror proto.ErrCode but live in state pkg so reducers can
// emit them without importing proto. The runtime translates them to
// proto.ErrCode values when serializing the response.
const (
	ErrCodeNotFound        = "not_found"
	ErrCodeInvalidArgument = "invalid_argument"
	ErrCodeInternal        = "internal"
	ErrCodeAlreadyExists   = "already_exists"
	ErrCodeUnsupported     = "unsupported"
)
