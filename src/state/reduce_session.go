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
	command := resolveCreateCommand(s, p.Command)
	sessID := allocSessionID()
	drv := GetDriver(command)
	if drv == nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeUnsupported, "no driver registered for command "+command)}
	}

	driverState, launch, setupJob, err := prepareSessionDriver(s, drv, sessID, p.Project, command)
	if err != nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, err.Error())}
	}

	session := Session{
		ID:        sessID,
		Project:   p.Project,
		Command:   command,
		CreatedAt: s.Now,
		Driver:    driverState,
	}

	if setupJob != nil {
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{StartedAt: s.Now}
		s.PendingCreates = clonePendingCreates(s.PendingCreates)
		s.PendingCreates[jobID] = PendingCreate{
			Session:    session,
			ReplyConn:  connID,
			ReplyReqID: reqID,
		}
		return s, []Effect{
			EffStartJob{JobID: jobID, Input: setupJob},
		}
	}

	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = session

	return s, []Effect{
		EffSpawnTmuxWindow{
			SessionID:  sessID,
			Project:    p.Project,
			Command:    launch.Command,
			StartDir:   launch.StartDir,
			Env:        map[string]string{"ROOST_SESSION_ID": string(sessID)},
			ReplyConn:  connID,
			ReplyReqID: reqID,
		},
	}
}

func resolveCreateCommand(s State, command string) string {
	if command == "" {
		command = s.DefaultCommand
	}
	if command == "" {
		command = "shell"
	}
	if expanded, ok := s.Aliases[command]; ok {
		command = expanded
	}
	return command
}

func prepareSessionDriver(s State, drv Driver, sessID SessionID, project, command string) (DriverState, CreateLaunch, JobInput, error) {
	driverState := drv.NewState(s.Now)
	launch := CreateLaunch{Command: command, StartDir: project}
	var setupJob JobInput
	if planner, ok := drv.(CreateSessionPlanner); ok {
		var plan CreatePlan
		var err error
		driverState, plan, err = planner.PrepareCreate(driverState, sessID, project, command)
		if err != nil {
			return nil, launch, nil, err
		}
		if plan.Launch.Command != "" {
			launch.Command = plan.Launch.Command
		}
		if plan.Launch.StartDir != "" {
			launch.StartDir = plan.Launch.StartDir
		}
		setupJob = plan.SetupJob
	}
	return driverState, launch, setupJob, nil
}

func reduceTmuxPaneSpawned(s State, e EvTmuxPaneSpawned) (State, []Effect) {
	if _, ok := s.Sessions[e.SessionID]; !ok {
		return s, nil
	}
	s.ActiveSession = e.SessionID

	effs := []Effect{
		EffRegisterPane{SessionID: e.SessionID, PaneTarget: e.PaneTarget},
		EffActivateSession{SessionID: e.SessionID, Reason: EventCreateSession},
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
	var effs []Effect
	if sess, ok := s.Sessions[e.SessionID]; ok {
		drv := GetDriver(sess.Command)
		if provider, ok := drv.(ManagedWorktreeProvider); ok {
			if path := provider.ManagedWorktreePath(sess.Driver); path != "" {
				effs = append(effs, EffRemoveManagedWorktree{Path: path})
			}
		}
	}
	if _, ok := s.Sessions[e.SessionID]; ok {
		s.Sessions = cloneSessions(s.Sessions)
		delete(s.Sessions, e.SessionID)
	}
	if e.ReplyConn == 0 {
		return s, effs
	}
	return s, append(effs,
		errResp(e.ReplyConn, e.ReplyReqID, ErrCodeInternal,
			fmt.Sprintf("tmux spawn failed: %s", e.Err)),
	)
}

func reduceStopSession(s State, connID ConnID, reqID string, p StopSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, sid)
	var deactivate []Effect
	if s.ActiveSession == sid {
		s.ActiveSession = ""
		deactivate = []Effect{EffDeactivateSession{}}
	}
	return s, append(deactivate, []Effect{
		EffKillSessionWindow{SessionID: sid},
		EffUnregisterPane{SessionID: sid},
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, nil),
	}...)
}

func reducePreviewSession(s State, connID ConnID, reqID string, p PreviewSessionParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	s.ActiveSession = sid

	return s, []Effect{
		EffActivateSession{SessionID: sid, Reason: EventPreviewSession},
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
		EffActivateSession{SessionID: sid, Reason: EventSwitchSession},
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
