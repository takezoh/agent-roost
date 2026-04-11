package state

// Job result reducer. Looks up the JobMeta to find which session the
// result belongs to, dispatches DEvJobResult to that session's
// driver, and clears the job from the in-flight map.

func reduceJobResult(s State, e EvJobResult) (State, []Effect) {
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
