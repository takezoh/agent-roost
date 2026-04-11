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
	if _, ok := s.Sessions[e.SessionID]; !ok {
		return s, nil
	}
	s.ActiveSession = e.SessionID

	effs := []Effect{
		EffRegisterWindow{SessionID: e.SessionID, WindowTarget: e.WindowTarget},
		EffActivateSession{SessionID: e.SessionID},
		EffSelectPane{Target: "{sessionName}:0.0"},
		EffSyncStatusLine{Line: ""},
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
	}
	if e.ReplyConn != 0 {
		effs = append(effs, okResp(e.ReplyConn, e.ReplyReqID, CreateSessionReply{
			SessionID: string(e.SessionID),
		}))
	}
	return s, effs
}

type CreateSessionReply struct {
	SessionID string
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
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, sid)
	if s.ActiveSession == sid {
		s.ActiveSession = ""
	}
	return s, []Effect{
		EffKillSessionWindow{SessionID: sid},
		EffUnregisterWindow{SessionID: sid},
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, nil),
	}
}

func reducePreviewSession(s State, connID ConnID, reqID string, p PreviewSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.ActiveSession = sid

	return s, []Effect{
		EffActivateSession{SessionID: sid},
		EffSyncStatusLine{Line: ""},
		EffBroadcastSessionsChanged{IsPreview: true},
		okResp(connID, reqID, ActiveSessionReply{ActiveSessionID: string(sid)}),
	}
}

func reduceSwitchSession(s State, connID ConnID, reqID string, p SwitchSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.ActiveSession = sid

	return s, []Effect{
		EffActivateSession{SessionID: sid},
		EffSelectPane{Target: "{sessionName}:0.0"},
		EffSyncStatusLine{Line: ""},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, ActiveSessionReply{ActiveSessionID: string(sid)}),
	}
}

type ActiveSessionReply struct {
	ActiveSessionID string
}

func reducePreviewProject(s State, connID ConnID, reqID string, p PreviewProjectParams) (State, []Effect) {
	var effs []Effect
	if s.ActiveSession != "" {
		effs = append(effs, EffDeactivateSession{})
		s.ActiveSession = ""
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
