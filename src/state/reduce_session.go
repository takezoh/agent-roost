package state

import "fmt"

// Session lifecycle reducers.
//
// reduceCreateSession is async-2-step: it allocates a session id and
// emits EffSpawnTmuxWindow with the caller's reply context. The
// runtime executes the spawn and feeds back EvTmuxWindowSpawned (or
// EvTmuxSpawnFailed) which carries the reply context, allowing
// reduceTmuxWindowSpawned to send the response and broadcast the
// new session list.

func reduceCreateSession(s State, e EvCmdCreateSession) (State, []Effect) {
	if e.Project == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "project arg required")}
	}
	command := e.Command
	if command == "" {
		command = "claude"
	}

	// Allocate session id and stub a session record. WindowID stays
	// empty until EvTmuxWindowSpawned arrives. The driver state is
	// initialized via the registered driver.
	sessID := allocSessionID()
	drv := GetDriver(command)
	if drv == nil {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeUnsupported, "no driver registered for command "+command)}
	}

	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = Session{
		ID:        sessID,
		Project:   e.Project,
		Command:   command,
		CreatedAt: s.Now,
		Driver:    drv.NewState(s.Now),
	}

	return s, []Effect{
		EffSpawnTmuxWindow{
			SessionID:  sessID,
			Project:    e.Project,
			Command:    command,
			StartDir:   e.Project,
			Env:        map[string]string{"ROOST_SESSION_ID": string(sessID)},
			ReplyConn:  e.ConnID,
			ReplyReqID: e.ReqID,
		},
	}
}

// reduceTmuxWindowSpawned is the second half of CreateSession. It
// fills in the WindowID/AgentPaneID, persists the snapshot, sends the
// response, and broadcasts the new session list.
func reduceTmuxWindowSpawned(s State, e EvTmuxWindowSpawned) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		// Session was stopped before the spawn callback fired. Bail
		// silently — the half-created window is the runtime's
		// responsibility to kill.
		return s, nil
	}
	s.Sessions = cloneSessions(s.Sessions)
	sess.WindowID = e.WindowID
	sess.AgentPaneID = e.AgentPaneID
	s.Sessions[e.SessionID] = sess

	effs := []Effect{
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
	}
	if e.ReplyConn != 0 {
		effs = append(effs, okResp(e.ReplyConn, e.ReplyReqID, CreateSessionReply{
			SessionID: string(e.SessionID),
			WindowID:  string(e.WindowID),
		}))
	}
	return s, effs
}

// CreateSessionReply is the response body the proto layer marshals
// into RespCreateSession. Defined in state pkg so reducers can build
// it without importing proto.
type CreateSessionReply struct {
	SessionID string
	WindowID  string
}

// reduceTmuxSpawnFailed evicts the half-created session and replies
// with an error.
func reduceTmuxSpawnFailed(s State, e EvTmuxSpawnFailed) (State, []Effect) {
	if _, ok := s.Sessions[e.SessionID]; ok {
		s.Sessions = cloneSessions(s.Sessions)
		delete(s.Sessions, e.SessionID)
	}
	if e.ReplyConn == 0 {
		return s, nil
	}
	return s, []Effect{
		errResp(e.ReplyConn, e.ReplyReqID, ErrCodeInternal,
			fmt.Sprintf("tmux spawn failed: %s", e.Err)),
	}
}

func reduceStopSession(s State, e EvCmdStopSession) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "session not found")}
	}
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, e.SessionID)
	if s.Active == sess.WindowID {
		s.Active = ""
	}
	return s, []Effect{
		EffKillTmuxWindow{WindowID: sess.WindowID},
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
		okResp(e.ConnID, e.ReqID, nil),
	}
}

func reducePreviewSession(s State, e EvCmdPreviewSession) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "session not found")}
	}
	if sess.WindowID == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "session has no tmux window yet")}
	}

	chain := buildSwapChain(s.Active, sess.WindowID)
	s.Active = sess.WindowID

	return s, []Effect{
		EffSwapPane{ChainOps: chain},
		EffSetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW", Value: string(sess.WindowID)},
		EffSyncStatusLine{Line: ""}, // runtime fills from view
		EffBroadcastSessionsChanged{IsPreview: true},
		okResp(e.ConnID, e.ReqID, ActiveWindowReply{ActiveWindowID: string(sess.WindowID)}),
	}
}

func reduceSwitchSession(s State, e EvCmdSwitchSession) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeNotFound, "session not found")}
	}
	if sess.WindowID == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "session has no tmux window yet")}
	}

	chain := buildSwapChain(s.Active, sess.WindowID)
	s.Active = sess.WindowID

	return s, []Effect{
		EffSwapPane{ChainOps: chain},
		EffSelectPane{Target: "{sessionName}:0.0"}, // runtime resolves placeholder
		EffSetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW", Value: string(sess.WindowID)},
		EffSyncStatusLine{Line: ""},
		EffBroadcastSessionsChanged{},
		okResp(e.ConnID, e.ReqID, ActiveWindowReply{ActiveWindowID: string(sess.WindowID)}),
	}
}

// ActiveWindowReply is the body returned by Preview/Switch/etc.
// Carries the new active window id back to the caller.
type ActiveWindowReply struct {
	ActiveWindowID string
}

func reducePreviewProject(s State, e EvCmdPreviewProject) (State, []Effect) {
	// Deactivate any currently swapped-in session so the main pane
	// goes back to the keybind help screen.
	var effs []Effect
	if s.Active != "" {
		chain := buildDeactivateChain(s.Active)
		effs = append(effs, EffSwapPane{ChainOps: chain})
		effs = append(effs, EffUnsetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW"})
		s.Active = ""
	}
	effs = append(effs, okResp(e.ConnID, e.ReqID, nil))
	effs = append(effs, EffBroadcastEvent{
		Name:    "project-selected",
		Payload: ProjectSelectedPayload{Project: e.Project},
	})
	return s, effs
}

// ProjectSelectedPayload is the body for the project-selected event
// the runtime broadcasts to subscribers.
type ProjectSelectedPayload struct {
	Project string
}

func reduceListSessions(s State, e EvCmdListSessions) (State, []Effect) {
	return s, []Effect{
		okResp(e.ConnID, e.ReqID, SessionsReply{}),
	}
}

// SessionsReply is a marker body — the runtime serializes it by
// reading State.Sessions directly and producing the proto SessionInfo
// list. State pkg has no knowledge of proto so we can't materialize
// SessionInfo here.
type SessionsReply struct{}

func reduceFocusPane(s State, e EvCmdFocusPane) (State, []Effect) {
	if e.Pane == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "pane arg required")}
	}
	return s, []Effect{
		EffSelectPane{Target: e.Pane},
		EffBroadcastEvent{
			Name:    "pane-focused",
			Payload: PaneFocusedPayload{Pane: e.Pane},
		},
		okResp(e.ConnID, e.ReqID, nil),
	}
}

// PaneFocusedPayload is the body for the pane-focused event.
type PaneFocusedPayload struct {
	Pane string
}

func reduceLaunchTool(s State, e EvCmdLaunchTool) (State, []Effect) {
	if e.Tool == "" {
		return s, []Effect{errResp(e.ConnID, e.ReqID, ErrCodeInvalidArgument, "tool arg required")}
	}
	return s, []Effect{
		EffDisplayPopup{
			Width:  "60%",
			Height: "50%",
			Tool:   e.Tool,
			Args:   e.Args,
		},
		okResp(e.ConnID, e.ReqID, nil),
	}
}

// === Swap-chain builders ===

// buildSwapChain returns the [][]string passed to EffSwapPane for a
// preview/switch operation. If active is non-empty, the chain swaps
// the current active window back first.
func buildSwapChain(active, target WindowID) [][]string {
	pane0 := "{sessionName}:0.0" // runtime substitutes session name
	var cmds [][]string
	if active != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", string(active) + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", string(target) + ".0"})
	return cmds
}

// buildDeactivateChain swaps the currently active window back to the
// background, leaving pane 0.0 with the keybind help.
func buildDeactivateChain(active WindowID) [][]string {
	pane0 := "{sessionName}:0.0"
	return [][]string{
		{"swap-pane", "-d", "-s", pane0, "-t", string(active) + ".0"},
	}
}
