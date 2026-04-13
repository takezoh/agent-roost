package state

// Job result reducer. Looks up the JobMeta to find which session the
// result belongs to, dispatches DEvJobResult to that session's
// driver, and clears the job from the in-flight map.

func reduceJobResult(s State, e EvJobResult) (State, []Effect) {
	if pending, ok := s.PendingCreates[e.JobID]; ok {
		return handlePendingCreate(s, pending, e)
	}

	meta, ok := s.Jobs[e.JobID]
	if !ok {
		// Stale result (session was stopped before the job finished).
		// Drop silently — the job's effects no longer matter.
		return s, nil
	}

	// Remove the job entry up front so a Step that races on the same
	// job kind doesn't see itself.
	s.Jobs = cloneJobs(s.Jobs)
	delete(s.Jobs, e.JobID)

	// Connector job — route to the connector's Step.
	if meta.Connector != "" {
		next, effs, ok := stepConnector(s, meta.Connector, CEvJobResult{
			Result: e.Result,
			Err:    e.Err,
			Now:    s.Now,
		})
		if !ok {
			return s, nil
		}
		effs = append(effs, EffBroadcastSessionsChanged{})
		return next, effs
	}

	// Driver job — route to the session's driver Step.
	next, effs, _, ok := stepDriver(s, meta.SessionID, DEvJobResult{
		Result: e.Result,
		Err:    e.Err,
		Now:    s.Now,
	})
	if !ok {
		return s, nil
	}
	s = next

	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs
}

func handlePendingCreate(s State, pending PendingCreate, e EvJobResult) (State, []Effect) {
	s.PendingCreates = clonePendingCreates(s.PendingCreates)
	delete(s.PendingCreates, e.JobID)
	s.Jobs = cloneJobs(s.Jobs)
	delete(s.Jobs, e.JobID)

	drv := GetDriver(pending.Session.Command)
	if drv == nil {
		return s, []Effect{errResp(pending.ReplyConn, pending.ReplyReqID, ErrCodeUnsupported, "no driver registered for command "+pending.Session.Command)}
	}
	planner, ok := drv.(CreateSessionPlanner)
	if !ok {
		return s, []Effect{errResp(pending.ReplyConn, pending.ReplyReqID, ErrCodeInternal, "driver missing create-session planner")}
	}

	nextDS, launch, err := planner.CompleteCreate(
		pending.Session.Driver,
		pending.Session.Command,
		pending.Session.LaunchOptions,
		e.Result,
		e.Err,
	)
	if err != nil {
		return s, []Effect{errResp(pending.ReplyConn, pending.ReplyReqID, ErrCodeInvalidArgument, err.Error())}
	}
	pending.Session.Driver = nextDS
	pending.Session.LaunchOptions = launch.Options
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[pending.Session.ID] = pending.Session
	return s, []Effect{
		EffSpawnTmuxWindow{
			SessionID:  pending.Session.ID,
			Mode:       LaunchModeCreate,
			Project:    pending.Session.Project,
			Command:    launch.Command,
			StartDir:   launch.StartDir,
			Options:    launch.Options,
			Env:        map[string]string{"ROOST_SESSION_ID": string(pending.Session.ID)},
			ReplyConn:  pending.ReplyConn,
			ReplyReqID: pending.ReplyReqID,
		},
	}
}

// reduceFileChanged routes a fsnotify event to the matching
// session's driver as DEvFileChanged.
func reduceFileChanged(s State, e EvFileChanged) (State, []Effect) {
	if _, ok := s.Sessions[e.SessionID]; !ok {
		return s, nil
	}
	next, effs, _, ok := stepDriver(s, e.SessionID, DEvFileChanged{Path: e.Path})
	if !ok {
		return s, nil
	}
	s = next
	if len(effs) > 0 {
		effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	}
	return s, effs
}
