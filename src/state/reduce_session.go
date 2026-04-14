package state

import (
	"encoding/json"
	"fmt"
)

type CreateSessionParams struct {
	Project string        `json:"project"`
	Command string        `json:"command"`
	Options LaunchOptions `json:"options,omitempty"`
}

type PushDriverParams struct {
	SessionID string        `json:"session_id"`
	Project   string        `json:"project"`
	Command   string        `json:"command"`
	Options   LaunchOptions `json:"options,omitempty"`
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
	RegisterEvent[PushDriverParams](EventPushDriver, reducePushDriver)
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

	driverState, setupJob, err := prepareSessionDriver(s, drv, sessID, p.Project, command, p.Options)
	if err != nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, err.Error())}
	}

	session := Session{
		ID:            sessID,
		Project:       p.Project,
		CreatedAt:     s.Now,
		Command:       command,
		LaunchOptions: p.Options,
		Driver:        driverState,
		Frames: []SessionFrame{{
			ID:            allocFrameID(),
			Project:       p.Project,
			Command:       command,
			LaunchOptions: p.Options,
			CreatedAt:     s.Now,
			Driver:        driverState,
		}},
	}
	frame := session.Frames[0]

	if setupJob != nil {
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{SessionID: sessID, FrameID: frame.ID, StartedAt: s.Now}
		s.PendingCreates = clonePendingCreates(s.PendingCreates)
		s.PendingCreates[jobID] = PendingCreate{
			Session:    session,
			FrameID:    frame.ID,
			ReplyConn:  connID,
			ReplyReqID: reqID,
		}
		return s, []Effect{
			EffStartJob{JobID: jobID, Input: setupJob},
		}
	}

	launch, err := drv.PrepareLaunch(driverState, LaunchModeCreate, p.Project, command, p.Options)
	if err != nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, err.Error())}
	}
	session.Frames[0].LaunchOptions = launch.Options

	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = session

	return s, []Effect{
		EffSpawnTmuxWindow{
			SessionID: sessID,
			FrameID:   frame.ID,
			Mode:      LaunchModeCreate,
			Project:   p.Project,
			Command:   launch.Command,
			StartDir:  launch.StartDir,
			Options:   launch.Options,
			Env: map[string]string{
				"ROOST_SESSION_ID": string(sessID),
				"ROOST_FRAME_ID":   string(frame.ID),
			},
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

func prepareSessionDriver(s State, drv Driver, sessID SessionID, project, command string, options LaunchOptions) (DriverState, JobInput, error) {
	driverState := drv.NewState(s.Now)
	var setupJob JobInput
	if planner, ok := drv.(CreateSessionPlanner); ok {
		var plan CreatePlan
		var err error
		driverState, plan, err = planner.PrepareCreate(driverState, sessID, project, command, options)
		if err != nil {
			return nil, nil, err
		}
		setupJob = plan.SetupJob
	}
	return driverState, setupJob, nil
}

func reducePushDriver(s State, connID ConnID, reqID string, p PushDriverParams) (State, []Effect) {
	sid := SessionID(p.SessionID)
	if sid == "" {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "session_id required")}
	}
	if _, ok := s.Sessions[sid]; !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	newS, effs, err := pushDriverInternal(s, sid, p.Project, p.Command, p.Options, connID, reqID)
	if err != nil {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, err.Error())}
	}
	return newS, effs
}

// pushDriverInternal is the shared implementation for pushing a new driver frame
// onto a session. Used by reducePushDriver (IPC) and reduceDriverHook (EffPushDriver).
func pushDriverInternal(s State, sid SessionID, project, rawCommand string, options LaunchOptions, connID ConnID, reqID string) (State, []Effect, error) {
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, nil, fmt.Errorf("session not found")
	}
	if project == "" {
		project = sess.Project
	}

	command := resolveCreateCommand(s, rawCommand)
	drv := GetDriver(command)
	if drv == nil {
		return s, nil, fmt.Errorf("no driver registered for command %s", command)
	}

	driverState, setupJob, err := prepareSessionDriver(s, drv, sid, project, command, options)
	if err != nil {
		return s, nil, err
	}

	// Inherit root frame's StartDir so the child frame starts in the same directory.
	if rootF, ok := rootFrame(sess); ok {
		rootDrv := GetDriver(rootF.Command)
		if rp, ok := rootDrv.(StartDirAware); ok {
			if parentDir := rp.StartDir(rootF.Driver); parentDir != "" {
				if wp, ok := drv.(StartDirAware); ok {
					driverState = wp.WithStartDir(driverState, parentDir)
				}
			}
		}
	}

	frame := SessionFrame{
		ID:            allocFrameID(),
		Project:       project,
		Command:       command,
		LaunchOptions: options,
		CreatedAt:     s.Now,
		Driver:        driverState,
	}
	sess.Frames = append(append([]SessionFrame(nil), sess.Frames...), frame)
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sid] = sess

	if setupJob != nil {
		s.NextJobID++
		jobID := s.NextJobID
		s.Jobs = cloneJobs(s.Jobs)
		s.Jobs[jobID] = JobMeta{SessionID: sid, FrameID: frame.ID, StartedAt: s.Now}
		s.PendingCreates = clonePendingCreates(s.PendingCreates)
		s.PendingCreates[jobID] = PendingCreate{Session: sess, FrameID: frame.ID, ReplyConn: connID, ReplyReqID: reqID}
		return s, []Effect{EffStartJob{JobID: jobID, Input: setupJob}}, nil
	}

	launch, err := drv.PrepareLaunch(driverState, LaunchModeCreate, project, command, options)
	if err != nil {
		return s, nil, err
	}
	sess.Frames[len(sess.Frames)-1].LaunchOptions = launch.Options
	s.Sessions[sid] = sess

	effs := []Effect{
		EffSpawnTmuxWindow{
			SessionID: sid,
			FrameID:   frame.ID,
			Mode:      LaunchModeCreate,
			Project:   project,
			Command:   launch.Command,
			StartDir:  launch.StartDir,
			Options:   launch.Options,
			Env: map[string]string{
				"ROOST_SESSION_ID": string(sid),
				"ROOST_FRAME_ID":   string(frame.ID),
			},
			ReplyConn:  connID,
			ReplyReqID: reqID,
		},
	}
	return s, effs, nil
}

func reduceTmuxPaneSpawned(s State, e EvTmuxPaneSpawned) (State, []Effect) {
	sess, ok := s.Sessions[e.SessionID]
	if !ok {
		return s, nil
	}
	if findFrameIndex(sess, e.FrameID) < 0 {
		return s, nil
	}
	s.ActiveSession = e.SessionID

	effs := []Effect{
		EffRegisterPane{FrameID: e.FrameID, PaneTarget: e.PaneTarget},
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
		if idx := findFrameIndex(sess, e.FrameID); idx >= 0 {
			frame := sess.Frames[idx]
			drv := GetDriver(frame.Command)
			if provider, ok := drv.(ManagedWorktreeProvider); ok {
				if path := provider.ManagedWorktreePath(frame.Driver); path != "" {
					effs = append(effs, EffRemoveManagedWorktree{Path: path})
				}
			}
			sess, _ = truncateFrames(sess, idx)
			s.Sessions = cloneSessions(s.Sessions)
			if len(sess.Frames) == 0 {
				delete(s.Sessions, e.SessionID)
			} else {
				s.Sessions[e.SessionID] = sess
			}
		}
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
	sess, ok := s.Sessions[sid]
	if !ok {
		return s, []Effect{errResp(connID, reqID, ErrCodeNotFound, "session not found")}
	}
	nextSess, removed := truncateFrames(sess, 0)
	s.Sessions = cloneSessions(s.Sessions)
	if len(nextSess.Frames) == 0 {
		delete(s.Sessions, sid)
	} else {
		s.Sessions[sid] = nextSess
	}
	var deactivate []Effect
	if s.ActiveSession == sid {
		s.ActiveSession = ""
		deactivate = []Effect{EffDeactivateSession{}}
	}
	// broadcast を先頭に置く — tmux kill が完了する前に TUI が更新される
	effs := []Effect{EffBroadcastSessionsChanged{}}
	effs = append(effs, deactivate...)
	for _, frame := range removed {
		effs = append(effs,
			EffKillSessionWindow{FrameID: frame.ID},
			EffUnregisterPane{FrameID: frame.ID},
			EffUnwatchFile{FrameID: frame.ID},
		)
	}
	effs = append(effs, okResp(connID, reqID, nil), EffPersistSnapshot{})
	return s, effs
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
