package state

import "github.com/takezoh/agent-roost/uiproc"

// Tick reducer. Fans the tick out to every session's driver and
// emits periodic reconciliation + health-check effects.

func reduceTick(s State, e EvTick) (State, []Effect) {
	s.Now = e.Now

	var seq uint64
	s, effs, changed := stepActiveSessions(s, func(sessID SessionID, sess Session, active bool) DriverEvent {
		frame, _ := activeFrame(sess)
		ev := DEvTick{
			Now:        e.Now,
			Active:     active,
			Project:    frame.Project,
			PaneTarget: e.PaneTargets[SessionID(frame.ID)],
			N:          e.N,
			Seq:        seq,
		}
		seq++
		return ev
	})

	// Initialize connectors (once).
	if !s.ConnectorsReady && len(AllConnectors()) > 0 {
		s.ConnectorsReady = true
		s.Connectors = cloneConnectors(s.Connectors)
		for _, c := range AllConnectors() {
			s.Connectors[c.Name()] = c.NewState()
		}
	}

	// Step all connectors.
	s, connEffs := stepConnectors(s)
	effs = append(effs, connEffs...)
	if len(connEffs) > 0 {
		changed = true
	}

	// Active-pane death check: every tick (covers the no-active-frame case
	// where the fast ticker skips; fast ticker handles the active-frame case).
	effs = append(effs, EffCheckPaneAlive{Pane: "{sessionName}:0.0"})

	// Control-pane health and window reconcile: every 5 ticks to reduce
	// subprocess pressure. These are non-latency-sensitive: a respawn
	// triggered 5 s late is indistinguishable from 1 s late for the user.
	if e.N%5 == 0 {
		effs = append(effs,
			EffCheckPaneAlive{Pane: "{sessionName}:0.1"},
			EffCheckPaneAlive{Pane: "{sessionName}:0.2"},
			EffReconcileWindows{},
		)
	}

	if changed {
		effs = append(effs, EffPersistSnapshot{}, EffBroadcastSessionsChanged{})
	}
	return s, effs
}

func stepConnectors(s State) (State, []Effect) {
	var effs []Effect
	for _, c := range AllConnectors() {
		next, cEffs, ok := stepConnector(s, c.Name(), CEvTick{Now: s.Now})
		if !ok {
			continue
		}
		s = next
		effs = append(effs, cEffs...)
	}
	return s, effs
}

// reducePaneDied handles a dead pane detected by EffCheckPaneAlive.
//   - Control panes (0.1 / 0.2): respawn the TUI process
//   - Pane 0.0 with no active session: respawn the main TUI
//   - Pane 0.0 with active session: evict the dead session, kill its
//     parked window, clear ActiveSession, then deactivate back to main
func reducePaneDied(s State, e EvPaneDied) (State, []Effect) {
	// Control pane respawn
	if proc, ok := uiproc.RespawnTarget(e.Pane); ok {
		return s, []Effect{
			EffRespawnPane{Pane: e.Pane, Proc: proc},
		}
	}

	// Pane 0.0 dead with no active session: main TUI crashed.
	if e.Pane == "{sessionName}:0.0" && s.ActiveSession == "" {
		return s, []Effect{
			EffRespawnPane{Pane: e.Pane, Proc: uiproc.Main()},
		}
	}

	// Pane 0.0 dead with active session: evict the owning session.
	// OwnerFrameID is set by the runtime; fall back to ActiveSession
	// if the runtime couldn't identify the owner via pane_id.
	ownerID := e.OwnerFrameID
	if ownerID == "" {
		if sess, ok := s.Sessions[s.ActiveSession]; ok {
			if frame, ok := activeFrame(sess); ok {
				ownerID = frame.ID
			}
		}
	}
	if ownerID == "" {
		return s, nil
	}
	s, effs, ok := evictFrame(s, ownerID, true)
	if !ok {
		return s, nil
	}
	return s, effs
}


// reduceTmuxWindowVanished evicts a session whose tmux window has
// disappeared (agent process exited) and broadcasts the new list.
// If the vanished session was active, deactivation restores the main TUI.
func reduceTmuxWindowVanished(s State, e EvTmuxWindowVanished) (State, []Effect) {
	s, effs, ok := evictFrame(s, e.FrameID, false)
	if !ok {
		return s, nil
	}
	return s, effs
}
