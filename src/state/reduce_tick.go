package state

// Tick reducer. Fans the tick out to every session's driver and
// emits the periodic health-check effects for the control panes.

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

	// Health-check the control panes (Pane 0.1 = log, 0.2 = sessions).
	effs = append(effs,
		EffCheckPaneAlive{Pane: "{sessionName}:0.1"},
		EffCheckPaneAlive{Pane: "{sessionName}:0.2"},
	)

	// Persist + broadcast so sessions.json always reflects the latest
	// driver state. The runtime's persist backend is cheap for small
	// files; if it ever becomes a bottleneck, add a dirty-flag check.
	effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	return s, effs
}

// reducePaneDied responds to the runtime detecting a dead control
// pane via EffCheckPaneAlive. Issues a respawn for the corresponding
// pane.
func reducePaneDied(s State, e EvPaneDied) (State, []Effect) {
	cmd := paneRespawnCommand(e.Pane)
	if cmd == "" {
		return s, nil
	}
	return s, []Effect{
		EffRespawnPane{Pane: e.Pane, Cmd: cmd},
	}
}

// paneRespawnCommand returns the command line the runtime should run
// when respawning a dead control pane. Empty for unknown panes (the
// reducer ignores them rather than guessing).
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
