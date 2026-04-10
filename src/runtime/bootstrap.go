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
			AgentPaneID: snap.AgentPaneID,
			CreatedAt:   createdAt,
			Driver:      drv.Restore(snap.DriverState, now),
		}
	}
	slog.Info("bootstrap: snapshot loaded", "count", len(snaps))
	return nil
}

// ReconcileWarm reads the live tmux window list and:
//   - Updates known sessions' WindowID/AgentPaneID from the live tmux state
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
		if w.AgentPaneID != "" {
			sess.AgentPaneID = w.AgentPaneID
		}
		// Driver state comes from sessions.json (loaded by
		// LoadSnapshot), NOT from tmux user options. @roost_id is
		// the only user option — everything else was migrated in
		// Phase 8.
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
		"@roost_agent_pane",
		"@roost_persisted_state",
	}
	for _, w := range windows {
		for _, key := range legacyKeys {
			u.UnsetWindowUserOption(w.WindowID, key)
		}
	}
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
		windowName := buildWindowName(sess.Project, string(id))
		wid, paneID, err := r.cfg.Tmux.SpawnWindow(
			windowName,
			"exec "+spawnCmd,
			sess.Project,
			map[string]string{"ROOST_SESSION_ID": string(id)},
		)
		if err != nil {
			slog.Error("bootstrap: spawn failed", "id", id, "err", err)
			delete(r.state.Sessions, id)
			continue
		}
		sess.WindowID = state.WindowID(wid)
		sess.AgentPaneID = paneID
		r.state.Sessions[id] = sess
	}
	return nil
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
