package state

import (
	"encoding/json"
	"fmt"
)

type CreateSessionParams struct {
	Project string `json:"project"`
	Command string `json:"command"`
}

type StopSessionParams struct {
	SessionID string `json:"session_id"`
}

type PreviewSessionParams struct {
	SessionID string `json:"session_id"`
}

type SwitchSessionParams struct {
	SessionID string `json:"session_id"`
}

type PreviewProjectParams struct {
	Project string `json:"project"`
}

type FocusPaneParams struct {
	Pane string `json:"pane"`
}

func init() {
	RegisterEvent[CreateSessionParams](EventCreateSession, reduceCreateSession)
	RegisterEvent[StopSessionParams](EventStopSession, reduceStopSession)
	RegisterEvent[struct{}](EventListSessions, reduceListSessions)
	RegisterEvent[PreviewSessionParams](EventPreviewSession, reducePreviewSession)
	RegisterEvent[SwitchSessionParams](EventSwitchSession, reduceSwitchSession)
	RegisterEvent[PreviewProjectParams](EventPreviewProject, reducePreviewProject)
	RegisterEvent[FocusPaneParams](EventFocusPane, reduceFocusPane)
	RegisterEvent[json.RawMessage](EventLaunchTool, reduceLaunchTool)
}

func reduceCreateSession(s State, connID ConnID, reqID string, p CreateSessionParams) (State, []Effect) {
	if p.Project == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "project arg required")}
	}
	command := p.Command
	if command == "" {
		command = s.DefaultCommand
	}
	if command == "" {
		command = "shell"
	}
	if expanded, ok := s.Aliases[command]; ok {
		command = expanded
	}

	sessID := allocSessionID()
	drv := GetDriver(command)
	if drv == nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeUnsupported, "no driver registered for command "+command)}
	}

	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = Session{
		ID:        sessID,
		Project:   p.Project,
		Command:   command,
		CreatedAt: s.Now,
		Driver:    drv.NewState(s.Now),
	}

	return s, []Effect{
		EffSpawnTmuxWindow{
			SessionID:  sessID,
			Project:    p.Project,
			Command:    command,
			StartDir:   p.Project,
			Env:        map[string]string{"ROOST_SESSION_ID": string(sessID)},
			ReplyConn:  connID,
			ReplyReqID: reqID,
		},
	}
}

func reduceTmuxWindowSpawned(s State, e EvTmuxWindowSpawned) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		return s, nil
	}
	s.Sessions = cloneSessions(s.Sessions)
	sess.WindowID = e.WindowID
	sess.PaneID = e.PaneID
	s.Sessions[e.SessionID] = sess

	chain := buildSwapChain(s.Active, e.WindowID)
	s.Active = e.WindowID

	effs := []Effect{
		EffSwapPane{ChainOps: chain},
		EffSelectPane{Target: "{sessionName}:0.0"},
		EffSetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW", Value: string(e.WindowID)},
		EffSyncStatusLine{Line: ""},
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

type CreateSessionReply struct {
	SessionID string
	WindowID  string
}

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

func reduceStopSession(s State, connID ConnID, reqID string, p StopSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, sid)
	if s.Active == sess.WindowID {
		s.Active = ""
	}
	return s, []Effect{
		EffKillTmuxWindow{WindowID: sess.WindowID},
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, nil),
	}
}

func reducePreviewSession(s State, connID ConnID, reqID string, p PreviewSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	if sess.WindowID == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "session has no tmux window yet")}
	}

	chain := buildSwapChain(s.Active, sess.WindowID)
	s.Active = sess.WindowID

	return s, []Effect{
		EffSwapPane{ChainOps: chain},
		EffSetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW", Value: string(sess.WindowID)},
		EffSyncStatusLine{Line: ""},
		EffBroadcastSessionsChanged{IsPreview: true},
		okResp(connID, reqID, ActiveWindowReply{ActiveWindowID: string(sess.WindowID)}),
	}
}

func reduceSwitchSession(s State, connID ConnID, reqID string, p SwitchSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	if sess.WindowID == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "session has no tmux window yet")}
	}

	chain := buildSwapChain(s.Active, sess.WindowID)
	s.Active = sess.WindowID

	return s, []Effect{
		EffSwapPane{ChainOps: chain},
		EffSelectPane{Target: "{sessionName}:0.0"},
		EffSetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW", Value: string(sess.WindowID)},
		EffSyncStatusLine{Line: ""},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, ActiveWindowReply{ActiveWindowID: string(sess.WindowID)}),
	}
}

type ActiveWindowReply struct {
	ActiveWindowID string
}

func reducePreviewProject(s State, connID ConnID, reqID string, p PreviewProjectParams) (State, []Effect) {
	var effs []Effect
	if s.Active != "" {
		chain := buildDeactivateChain(s.Active)
		effs = append(effs, EffSwapPane{ChainOps: chain})
		effs = append(effs, EffUnsetTmuxEnv{Key: "ROOST_ACTIVE_WINDOW"})
		s.Active = ""
	}
	effs = append(effs, okResp(connID, reqID, nil))
	effs = append(effs, EffBroadcastEvent{
		Name:    "project-selected",
		Payload: ProjectSelectedPayload{Project: p.Project},
	})
	return s, effs
}

type ProjectSelectedPayload struct {
	Project string
}

func reduceListSessions(s State, connID ConnID, reqID string, _ struct{}) (State, []Effect) {
	return s, []Effect{
		okResp(connID, reqID, SessionsReply{}),
	}
}

type SessionsReply struct{}

func reduceFocusPane(s State, connID ConnID, reqID string, p FocusPaneParams) (State, []Effect) {
	if p.Pane == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "pane arg required")}
	}
	return s, []Effect{
		EffSelectPane{Target: p.Pane},
		EffBroadcastEvent{
			Name:    "pane-focused",
			Payload: PaneFocusedPayload{Pane: p.Pane},
		},
		okResp(connID, reqID, nil),
	}
}

type PaneFocusedPayload struct {
	Pane string
}

func reduceLaunchTool(s State, connID ConnID, reqID string, raw json.RawMessage) (State, []Effect) {
	var m map[string]string
	if len(raw) > 0 {
		json.Unmarshal(raw, &m)
	}
	tool := m["tool"]
	if tool == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "tool arg required")}
	}
	delete(m, "tool")
	return s, []Effect{
		EffDisplayPopup{
			Width:  "60%",
			Height: "50%",
			Tool:   tool,
			Args:   m,
		},
		okResp(connID, reqID, nil),
	}
}

// === Swap-chain builders ===

func buildSwapChain(active, target WindowID) [][]string {
	pane0 := "{sessionName}:0.0"
	var cmds [][]string
	if active != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", string(active) + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", string(target) + ".0"})
	return cmds
}

func buildDeactivateChain(active WindowID) [][]string {
	pane0 := "{sessionName}:0.0"
	return [][]string{
		{"swap-pane", "-d", "-s", pane0, "-t", string(active) + ".0"},
	}
}
