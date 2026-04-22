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
	if sess.ActiveFrameID != "" {
		for _, f := range sess.Frames {
			if f.ID == sess.ActiveFrameID {
				return f, true
			}
		}
	}
	return sess.Frames[len(sess.Frames)-1], true
}

// pushMRU prepends frameID to sess.MRUFrameIDs, capped at 16 entries.
func pushMRU(sess Session, frameID FrameID) Session {
	if frameID == "" {
		return sess
	}
	mru := make([]FrameID, 0, len(sess.MRUFrameIDs)+1)
	mru = append(mru, frameID)
	for _, id := range sess.MRUFrameIDs {
		if id != frameID {
			mru = append(mru, id)
		}
	}
	if len(mru) > 16 {
		mru = mru[:16]
	}
	sess.MRUFrameIDs = mru
	return sess
}

// popMRU returns the first MRU frame that still exists in sess, or "" if none.
// It also trims stale entries from the front of MRUFrameIDs.
func popMRU(sess Session) (FrameID, Session) {
	existing := make(map[FrameID]bool, len(sess.Frames))
	for _, f := range sess.Frames {
		existing[f.ID] = true
	}
	for i, id := range sess.MRUFrameIDs {
		if existing[id] {
			sess.MRUFrameIDs = append([]FrameID(nil), sess.MRUFrameIDs[i+1:]...)
			return id, sess
		}
	}
	sess.MRUFrameIDs = nil
	return "", sess
}

// removeFrameByIndex removes the frame at position i, preserving all others.
func removeFrameByIndex(sess Session, i int) (Session, SessionFrame) {
	removed := sess.Frames[i]
	frames := make([]SessionFrame, 0, len(sess.Frames)-1)
	frames = append(frames, sess.Frames[:i]...)
	frames = append(frames, sess.Frames[i+1:]...)
	sess.Frames = frames
	return sess, removed
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
func stepDriver(s State, frameID FrameID, ev DriverEvent) (State, []Effect, bool) { //nolint:funlen
	sessID, sess, frameIdx, ok := findFrame(s, frameID)
	if !ok {
		return s, nil, false
	}
	frame := sess.Frames[frameIdx]
	drv := GetDriver(frame.Command)
	if drv == nil {
		return s, nil, false
	}

	ctx := FrameContext{
		ID:            frame.ID,
		Project:       frame.Project,
		Command:       frame.Command,
		LaunchOptions: frame.LaunchOptions,
		CreatedAt:     frame.CreatedAt,
		IsRoot:        frameIdx == 0,
	}

	oldStatus := drv.Status(frame.Driver)
	nextDS, rawEffs, _ := drv.Step(frame.Driver, ctx, ev)

	s.Sessions = cloneSessions(s.Sessions)
	sess.Frames = append([]SessionFrame(nil), sess.Frames...)
	frame.Driver = nextDS
	sess.Frames[frameIdx] = frame
	s.Sessions[sessID] = sess

	out := make([]Effect, 0, len(rawEffs))
	for _, eff := range rawEffs {
		patched, newState := postProcessEffect(s, sessID, frameID, eff)
		s = newState
		out = append(out, patched)
	}

	newStatus := drv.Status(nextDS)
	if kind, ok := ClassifyStatusTransition(oldStatus, newStatus); ok {
		out = append(out, EffNotify{
			SessionID: sessID,
			FrameID:   frameID,
			Driver:    drv.Name(),
			Command:   FirstToken(frame.Command),
			Project:   sess.Project,
			Kind:      kind,
			OldStatus: oldStatus,
			NewStatus: newStatus,
		})
	}

	// If the frame just became idle, drain its peer inbox.
	if newStatus != oldStatus {
		var injectEffs []Effect
		s, injectEffs = drainPeerInbox(s, sessID, frameID, oldStatus, newStatus)
		out = append(out, injectEffs...)
	}

	return s, out, true
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
	case EffRecordNotification:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e, s
	default:
		return eff, s
	}
}

// stepActiveSessions runs Step against every live session's driver.
// Each driver decides internally whether to react to a tick — return
// a no-op Step result to skip. Returns whether any session emitted
// effects, so the caller can decide whether to broadcast/persist.
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
		frame, ok := rootFrame(sess)
		if !ok {
			continue
		}
		drv := GetDriver(frame.Command)
		if drv == nil {
			continue
		}
		active := sessID == s.ActiveSession
		ev := makeEv(sessID, sess, active)
		next, sessEffs, ok := stepDriver(s, frame.ID, ev)
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

// evictFrame removes a frame and its cleanup effects.
//
// When frameID is the root frame (index 0), all sibling frames are also
// removed and the session is deleted — root death ends the session.
// When frameID is a child frame (index > 0), only that frame is removed;
// siblings are unaffected.
//
// killWindow controls whether EffKillSessionWindow is emitted per removed
// frame. Pass true when the tmux window still exists (e.g. EvPaneDied);
// pass false when the window has already vanished (e.g. EvTmuxWindowVanished).
//
// Effect ordering: deactivate → reactivate → cleanup → persist → broadcast.
// Reactivate precedes cleanup so that EffActivateSession swaps the parent
// pane into 0.1 before EffKillSessionWindow destroys the old window —
// preventing kill-window from targeting window 0.
func evictFrame(s State, frameID FrameID, killWindow bool) (State, []Effect, bool) {
	sessID, sess, idx, ok := findFrame(s, frameID)
	if !ok {
		return s, nil, false
	}
	if idx == 0 {
		return evictRootFrame(s, sessID, sess, killWindow)
	}
	return evictChildFrame(s, sessID, sess, idx, frameID, killWindow)
}

func evictRootFrame(s State, sessID SessionID, sess Session, killWindow bool) (State, []Effect, bool) {
	_, allRemoved := truncateFrames(sess, 0)
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, sessID)
	var effs []Effect
	if s.ActiveSession == sessID {
		s.ActiveSession = ""
		if s.ActiveOccupant == OccupantFrame {
			s.ActiveOccupant = OccupantMain
			effs = append(effs, EffDeactivateSession{})
		}
	}
	for _, frame := range allRemoved {
		if killWindow {
			effs = append(effs, EffKillSessionWindow{FrameID: frame.ID})
		}
		effs = append(effs, EffUnregisterPane{FrameID: frame.ID}, EffUnwatchFile{FrameID: frame.ID})
	}
	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs, true
}

func evictChildFrame(s State, sessID SessionID, sess Session, idx int, frameID FrameID, killWindow bool) (State, []Effect, bool) {
	wasActive := sess.ActiveFrameID == frameID
	sess, removed := removeFrameByIndex(sess, idx)
	if wasActive {
		fallback, next := popMRU(sess)
		sess = next
		if fallback == "" {
			fallback = sess.Frames[0].ID
		}
		sess.ActiveFrameID = fallback
	}
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = sess

	var effs []Effect
	if s.ActiveSession == sessID && wasActive {
		var pre []Effect
		s, pre = ensureMainAtVisibleSlot(s)
		s.ActiveOccupant = OccupantFrame
		effs = append(effs, pre...)
		effs = append(effs, EffActivateSession{SessionID: sessID, Reason: EventSwitchSession})
	}
	if killWindow {
		effs = append(effs, EffKillSessionWindow{FrameID: removed.ID})
	}
	effs = append(effs, EffUnregisterPane{FrameID: removed.ID}, EffUnwatchFile{FrameID: removed.ID})
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
