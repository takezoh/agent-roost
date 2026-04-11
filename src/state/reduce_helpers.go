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
func stepDriver(s State, sessID SessionID, ev DriverEvent) (State, []Effect, View, bool) {
	sess, ok := s.Sessions[sessID]
	if !ok {
		return s, nil, View{}, false
	}
	drv := GetDriver(sess.Command)
	if drv == nil {
		return s, nil, View{}, false
	}
	nextDS, rawEffs, view := drv.Step(sess.Driver, ev)

	s.Sessions = cloneSessions(s.Sessions)
	sess.Driver = nextDS
	s.Sessions[sessID] = sess

	if len(rawEffs) == 0 {
		return s, nil, view, true
	}

	out := make([]Effect, 0, len(rawEffs))
	for _, eff := range rawEffs {
		patched, newState := postProcessEffect(s, sessID, eff)
		s = newState
		out = append(out, patched)
	}
	return s, out, view, true
}

// postProcessEffect fills in session-context fields the driver Step
// left blank and (for EffStartJob) registers JobMeta + assigns a fresh
// JobID. Returns the patched effect and the (possibly mutated) State.
func postProcessEffect(s State, sessID SessionID, eff Effect) (Effect, State) {
	switch e := eff.(type) {
	case EffStartJob:
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{
			SessionID: sessID,
			StartedAt: s.Now,
		}
		e.JobID = jobID
		return e, s
	case EffEventLogAppend:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e, s
	case EffWatchFile:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e, s
	case EffUnwatchFile:
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
func stepActiveSessions(s State, makeEv func(sess Session, active bool) DriverEvent) (State, []Effect, bool) {
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
		drv := GetDriver(sess.Command)
		if drv == nil {
			continue
		}
		status := drv.Status(sess.Driver)
		if status == StatusIdle || status == StatusStopped {
			continue
		}
		active := sess.WindowID != "" && sess.WindowID == s.Active
		ev := makeEv(sess, active)
		next, sessEffs, _, ok := stepDriver(s, sessID, ev)
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
