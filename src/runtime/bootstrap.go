package runtime

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/tmux"
)

// Bootstrap helpers used at startup before the event loop starts.
// These mutate r.state directly — safe because no goroutine is
// reading state yet.

// LoadSnapshot reads sessions.json and registers each session in
// r.state. Per-session driver state is restored via the registered
// Driver's Restore method. Window IDs are NOT trusted yet — warm
// restart will refill them from the live tmux window list, cold
// restart will assign new ones during Recreate.
func (r *Runtime) LoadSnapshot() error {
	snaps, err := r.cfg.Persist.Load()
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return nil
	}
	now := time.Now()
	for _, snap := range snaps {
		drv := state.GetDriver(snap.Command)
		if drv == nil {
			slog.Warn("bootstrap: no driver for command, skipping", "command", snap.Command)
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, snap.CreatedAt)
		if createdAt.IsZero() {
			createdAt = now
		}
		r.state.Sessions[state.SessionID(snap.ID)] = state.Session{
			ID:          state.SessionID(snap.ID),
			Project:     snap.Project,
			Command:     snap.Command,
			WindowID:    state.WindowID(snap.WindowID),
			PaneID: snap.PaneID,
			CreatedAt:   createdAt,
			Driver:      drv.Restore(snap.DriverState, now),
		}
	}
	slog.Info("bootstrap: snapshot loaded", "count", len(snaps))
	return nil
}

// SetAliases sets the command alias map on state. Called once at
// startup from main.go with the config's [session] aliases.
func (r *Runtime) SetAliases(aliases map[string]string) {
	r.state.Aliases = aliases
}

// ReconcileWarm reads the live tmux window list and:
//   - Updates known sessions' WindowID/PaneID from the live tmux state
//     (tmux reissues window ids after server restart, so JSON values may be
//     stale).
//   - Drops orphan windows (in tmux but not in JSON).
//   - Drops orphan JSON sessions (in JSON but not in tmux).
//
// sessions.json is the single source of truth for all session data
// (project, command, driver state). tmux user options carry only
// @roost_id as a window-to-session marker.
//
// Called only on warm-restart (when the tmux session is still alive).
func (r *Runtime) ReconcileWarm() error {
	list, err := r.listRoostWindows()
	if err != nil {
		return err
	}
	live := make(map[state.SessionID]tmux.RoostWindow, len(list))
	for _, w := range list {
		live[state.SessionID(w.ID)] = w
	}

	var dropped []state.SessionID
	for id, sess := range r.state.Sessions {
		w, ok := live[id]
		if !ok {
			dropped = append(dropped, id)
			continue
		}
		sess.WindowID = state.WindowID(w.WindowID)
		// Re-query the pane ID since tmux may have reissued it
		// after a server restart (warm-restart within the same tmux
		// server keeps the same pane IDs, but we re-query anyway for
		// robustness).
		if paneID := r.queryPaneID(w.WindowID); paneID != "" {
			sess.PaneID = paneID
		}
		r.state.Sessions[id] = sess
	}
	for _, id := range dropped {
		slog.Warn("bootstrap: dropping orphan json session", "id", id)
		delete(r.state.Sessions, id)
	}

	// Clean up legacy user options from pre-Phase-8 installs. Each
	// window should carry only @roost_id; any @roost_project,
	// @roost_command, etc. left from the old code are ballast.
	r.cleanupLegacyUserOptions(list)
	return nil
}

// cleanupLegacyUserOptions removes pre-Phase-8 tmux user options that
// are no longer used. Called once per warm-restart.
func (r *Runtime) cleanupLegacyUserOptions(windows []tmux.RoostWindow) {
	type unsetter interface {
		UnsetWindowUserOption(windowID, key string) error
	}
	u, ok := r.cfg.Tmux.(unsetter)
	if !ok {
		return
	}
	legacyKeys := []string{
		"@roost_project",
		"@roost_command",
		"@roost_created_at",
		"@roost_pane",
		"@roost_persisted_state",
	}
	for _, w := range windows {
		for _, key := range legacyKeys {
			u.UnsetWindowUserOption(w.WindowID, key)
		}
	}
}

// DeactivateOnStartup swaps the previously active session back out
// of pane 0.0 so the main TUI (keybind help) is shown on startup.
// Must be called after ReconcileWarm (so WindowIDs are up to date).
func (r *Runtime) DeactivateOnStartup(tmuxClient interface{ GetEnv(string) (string, error) }) {
	wid, _ := tmuxClient.GetEnv("ROOST_ACTIVE_WINDOW")
	if wid == "" {
		return
	}
	found := false
	for _, sess := range r.state.Sessions {
		if string(sess.WindowID) == wid {
			found = true
			break
		}
	}
	if !found {
		return
	}
	pane0 := r.cfg.SessionName + ":0.0"
	op := []string{"swap-pane", "-d", "-s", pane0, "-t", wid + ".0"}
	if err := r.cfg.Tmux.RunChain(op); err != nil {
		slog.Warn("bootstrap: swap-pane failed during deactivate", "window", wid, "err", err)
	}
	_ = r.cfg.Tmux.UnsetEnv("ROOST_ACTIVE_WINDOW")
	_ = r.cfg.Tmux.SetStatusLine("")
	r.state.Active = ""
	slog.Info("bootstrap: deactivated session on startup", "window", wid)
}

// ClearStaleWindowIDs zeroes out WindowID and PaneID on every
// session. Called on cold start before RecreateAll so that stale
// IDs from the previous run's snapshot don't leak into the new
// tmux session (the old windows no longer exist).
func (r *Runtime) ClearStaleWindowIDs() {
	for id, sess := range r.state.Sessions {
		sess.WindowID = ""
		sess.PaneID = ""
		r.state.Sessions[id] = sess
	}
	r.state.Active = ""
}

// RecreateAll spawns fresh tmux windows for every session in
// r.state. Used during cold-start (the tmux session was just
// created and contains no roost windows yet).
func (r *Runtime) RecreateAll() error {
	for id, sess := range r.state.Sessions {
		drv := state.GetDriver(sess.Command)
		if drv == nil {
			continue
		}
		spawnCmd := drv.SpawnCommand(sess.Driver, sess.Command)
		startDir := sess.Project
		if bag := drv.Persist(sess.Driver); bag != nil {
			if wd := bag["working_dir"]; wd != "" {
				startDir = wd
			}
		}
		windowName := buildWindowName(sess.Project, string(id))
		tmuxCmd := "exec " + spawnCmd
		if isShellCommand(sess.Command) {
			tmuxCmd = ""
		}
		wid, paneID, err := r.cfg.Tmux.SpawnWindow(
			windowName,
			tmuxCmd,
			startDir,
			map[string]string{"ROOST_SESSION_ID": string(id)},
		)
		if err != nil {
			slog.Error("bootstrap: spawn failed", "id", id, "err", err)
			delete(r.state.Sessions, id)
			continue
		}
		sess.WindowID = state.WindowID(wid)
		sess.PaneID = paneID
		r.state.Sessions[id] = sess
	}

	// Give spawned processes a moment to start (or fail), then
	// reconcile to evict sessions whose windows died immediately
	// (e.g. claude --resume fails because the session no longer
	// exists). Without this, the TUI would show dead sessions until
	// the first periodic tick fires the reaper.
	time.Sleep(500 * time.Millisecond)
	r.reconcileAfterRecreate()
	return nil
}

// reconcileAfterRecreate checks each session's window and evicts any
// that died during RecreateAll. Uses the same ListRoostWindows path
// as ReconcileWarm.
func (r *Runtime) reconcileAfterRecreate() {
	list, err := r.listRoostWindows()
	if err != nil {
		return
	}
	live := make(map[string]struct{}, len(list))
	for _, w := range list {
		live[w.WindowID] = struct{}{}
	}
	var dead []state.SessionID
	for id, sess := range r.state.Sessions {
		if sess.WindowID == "" {
			continue
		}
		if _, ok := live[string(sess.WindowID)]; !ok {
			dead = append(dead, id)
		}
	}
	for _, id := range dead {
		slog.Info("bootstrap: evicting session with dead window", "id", id)
		delete(r.state.Sessions, id)
	}
}

// SetSyncCallbacks installs the optional tmux sync callbacks
// (currently a no-op since runtime handles tmux env vars and status
// line directly via effects). Kept as a stable hook so future Phase
// 9 tweaks land in one place.
func (r *Runtime) SetSyncCallbacks(active, status func(string)) {
	// Reserved.
}

// listRoostWindows extracts the typed RoostWindow list from the
// configured tmux backend, if it supports the optional interface
// (RealTmuxBackend does, fakes don't).
func (r *Runtime) listRoostWindows() ([]tmux.RoostWindow, error) {
	type lister interface {
		ListRoostWindows() ([]tmux.RoostWindow, error)
	}
	if l, ok := r.cfg.Tmux.(lister); ok {
		return l.ListRoostWindows()
	}
	return nil, nil
}

// queryPaneID asks tmux for the pane ID of a window's primary pane.
func (r *Runtime) queryPaneID(windowID string) string {
	type displayer interface {
		DisplayMessage(target, format string) (string, error)
	}
	d, ok := r.cfg.Tmux.(displayer)
	if !ok {
		return ""
	}
	out, err := d.DisplayMessage(windowID+".0", "#{pane_id}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// decodePersistedState parses the JSON-encoded @roost_persisted_state
// blob into the bag the driver Restore method expects.
func decodePersistedState(s string) map[string]string {
	if s == "" {
		return nil
	}
	var bag map[string]string
	if err := json.Unmarshal([]byte(s), &bag); err != nil {
		return nil
	}
	return bag
}

// buildWindowName mirrors the legacy SessionService format so warm
// restarts find their windows by the same name.
func buildWindowName(project, sessionID string) string {
	if i := strings.LastIndex(project, "/"); i >= 0 {
		project = project[i+1:]
	}
	if project == "" {
		project = "session"
	}
	return fmt.Sprintf("%s:%s", project, sessionID)
}
