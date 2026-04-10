package state

// Tick reducer. Fans the tick out to every session's driver and
// emits periodic reconciliation + health-check effects.

func reduceTick(s State, e EvTick) (State, []Effect) {
	s.Now = e.Now

	s, effs := stepAllSessions(s, func(sess Session, active bool) DriverEvent {
		return DEvTick{
			Now:      e.Now,
			Active:   active,
			Project:  sess.Project,
			WindowID: sess.WindowID,
		}
	})

	// Reconcile: compare live tmux windows with state sessions.
	// Any session whose window has vanished gets EvTmuxWindowVanished.
	effs = append(effs, EffReconcileWindows{})

	// Check pane 0.0 for a dead agent pane (active session's process
	// exited while swapped into the main pane). Control panes 0.1/0.2
	// are also checked for health-monitor respawn.
	effs = append(effs,
		EffCheckPaneAlive{Pane: "{sessionName}:0.0"},
		EffCheckPaneAlive{Pane: "{sessionName}:0.1"},
		EffCheckPaneAlive{Pane: "{sessionName}:0.2"},
	)

	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs
}

// reducePaneDied handles a dead pane detected by EffCheckPaneAlive.
//   - Control panes (0.1 / 0.2): respawn the TUI process
//   - Pane 0.0 with OwnerSessionID: evict the dead session, swap back,
//     kill its window, clear Active
func reducePaneDied(s State, e EvPaneDied) (State, []Effect) {
	// Control pane respawn
	if cmd := paneRespawnCommand(e.Pane); cmd != "" {
		return s, []Effect{
			EffRespawnPane{Pane: e.Pane, Cmd: cmd},
		}
	}

	// Pane 0.0 dead: evict the owning session
	if e.OwnerSessionID == "" {
		return s, nil
	}
	sess, ok := s.Sessions[e.OwnerSessionID]
	if !ok {
		return s, nil
	}

	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, e.OwnerSessionID)

	var effs []Effect
	// Swap the dead pane back to its origin window then kill it
	if sess.WindowID != "" {
		pane0 := "{sessionName}:0.0"
		effs = append(effs, EffSwapPane{ChainOps: [][]string{
			{"swap-pane", "-d", "-s", pane0, "-t", string(sess.WindowID) + ".0"},
		}})
		effs = append(effs, EffKillTmuxWindow{WindowID: sess.WindowID})
	}
	if s.Active == sess.WindowID {
		s.Active = ""
	}
	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs
}

func paneRespawnCommand(pane string) string {
	switch pane {
	case "{sessionName}:0.1":
		return "{roostExe} --tui log"
	case "{sessionName}:0.2":
		return "{roostExe} --tui sessions"
	}
	return ""
}

// reduceTmuxWindowVanished evicts a session whose tmux window has
// disappeared (agent process exited) and broadcasts the new list.
func reduceTmuxWindowVanished(s State, e EvTmuxWindowVanished) (State, []Effect) {
	var removedID SessionID
	for id, sess := range s.Sessions {
		if sess.WindowID == e.WindowID {
			removedID = id
			break
		}
	}
	if removedID == "" {
		return s, nil
	}
	s.Sessions = cloneSessions(s.Sessions)
	delete(s.Sessions, removedID)
	if s.Active == e.WindowID {
		s.Active = ""
	}
	return s, []Effect{
		EffPersistSnapshot{},
		EffBroadcastSessionsChanged{},
	}
}
