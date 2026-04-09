package core

import (
	"log/slog"
	"strings"
)

// ReapDeadSessions removes sessions whose tmux pane has died. Two paths
// converge into the ReconcileWindows cleanup loop:
//
//  1. Active-pane path: the agent pane lives in SESSION:0.0 (swap-pane'd in
//     by Preview/Switch). Window 0 has remain-on-exit on so a dead agent
//     lingers as `[exited]` instead of vanishing. We poll pane_dead and
//     pane_id on 0.0 together; if dead, the pane_id tells us *which* session
//     owns the dead pane (pane ids are stable across swap-pane). We then
//     swap that pane back into its owner window and kill the window. Note:
//     we never trust s.activeWindowID here, because concurrent Preview calls
//     can leave activeWindowID out of sync with the actual pane 0.0 owner —
//     the pane id is the only reliable identity.
//  2. Background sessions: the session window itself disappears (single-pane
//     window with remain-on-exit off).
//
// In both cases, the session window ends up gone and ReconcileWindows
// finalizes the in-memory cache cleanup. Returns the reaped session IDs so
// the caller can decide whether to broadcast a sessions-changed event.
func (s *Service) ReapDeadSessions() []string {
	s.reapDeadPane00()

	removed, err := s.Manager.ReconcileWindows()
	if err != nil {
		slog.Warn("reconcile windows failed", "err", err)
		return nil
	}
	if len(removed) == 0 {
		return nil
	}
	ids := make([]string, 0, len(removed))
	for _, r := range removed {
		s.ClearActive(r.WindowID)
		s.AgentStore.Unbind(r.WindowID)
		// Observer + state.Store entry must follow the Manager cache so the
		// next polling tick doesn't try to capture-pane on a dead window.
		s.Observers.Remove(r.WindowID)
		ids = append(ids, r.ID)
	}
	return ids
}

// reapDeadPane00 inspects pane SESSION:0.0 and, if its pane is dead, swaps
// the dead pane back into the owning session's window and kills that window.
// The owner is identified by the pane id reported by tmux (stable across
// swap-pane), NOT by s.activeWindowID — see ReapDeadSessions docs for why.
func (s *Service) reapDeadPane00() {
	out, err := s.Panes.DisplayMessage(s.SessionName+":0.0", "#{pane_dead} #{pane_id}")
	if err != nil {
		return
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 || fields[0] != "1" {
		return
	}
	deadPaneID := fields[1]

	sess := s.Manager.FindByAgentPaneID(deadPaneID)
	if sess == nil {
		// No session owns this pane — could be the main TUI itself dying,
		// or a legacy session created before pane id tracking. Leave the
		// reaper out of it; ReconcileWindows handles window-disappearance
		// cases on its own.
		slog.Warn("dead pane has no matching session", "pane", deadPaneID)
		return
	}

	slog.Info("reap dead pane", "session", sess.ID, "window", sess.WindowID, "pane", deadPaneID)
	if err := s.swapPaneBackTo(sess.WindowID); err != nil {
		slog.Warn("reap dead pane: swap-back failed", "window", sess.WindowID, "err", err)
		return
	}
	// Only clear activeWindowID when the session we just reaped is in fact
	// the one the user thought was active. If they don't match, leave
	// activeWindowID alone so the user keeps seeing the session they
	// selected — concurrent Preview races shouldn't kick the user off the
	// pane they're looking at.
	if s.activeWindowID == sess.WindowID {
		s.setActiveWindowID("")
	}
	if err := s.Manager.KillWindow(sess.WindowID); err != nil {
		slog.Warn("reap dead pane: kill window failed", "window", sess.WindowID, "err", err)
	}
	// Drop the observer immediately so the next polling tick (which can
	// run before ReconcileWindows finalizes the in-memory cache) doesn't
	// touch a dead window. ReconcileWindows will finalize the Manager
	// cache cleanup independently.
	s.Observers.Remove(sess.WindowID)
}

// swapPaneBackTo swaps whatever is currently in SESSION:0.0 back into the
// given session window's pane 0. Used by reapDeadPane00 to move a dead agent
// pane out of the main window 0 (which has remain-on-exit on) and into the
// session window so KillWindow can finish the cleanup. Independent of
// s.activeWindowID by design.
func (s *Service) swapPaneBackTo(windowID string) error {
	pane0 := s.SessionName + ":0.0"
	return s.Panes.RunChain([]string{"swap-pane", "-d", "-s", pane0, "-t", windowID + ".0"})
}
