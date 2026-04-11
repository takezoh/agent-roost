package runtime

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// Bootstrap helpers used at startup before the event loop starts.
// These mutate r.state and r.windowMap directly — safe because no
// goroutine is reading state yet.

// LoadSnapshot reads sessions.json and registers each session in
// r.state. Driver state is restored via the registered Driver's
// Restore method.
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
			ID:        state.SessionID(snap.ID),
			Project:   snap.Project,
			Command:   snap.Command,
			CreatedAt: createdAt,
			Driver:    drv.Restore(snap.DriverState, now),
		}
	}
	slog.Info("bootstrap: snapshot loaded", "count", len(snaps))
	return nil
}

// LoadWindowMap reads the ROOST_W_* tmux session environment variables
// and populates r.windowMap. Called on warm start after LoadSnapshot.
func (r *Runtime) LoadWindowMap() error {
	type envLister interface {
		ShowEnvironment() (string, error)
	}
	el, ok := r.cfg.Tmux.(envLister)
	if !ok {
		return nil
	}
	out, err := el.ShowEnvironment()
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ROOST_W_") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		idx := strings.TrimPrefix(parts[0], "ROOST_W_")
		sessID := state.SessionID(parts[1])
		r.windowMap[sessID] = idx
	}
	return nil
}

// ReconcileOrphans compares the loaded windowMap against the snapshot
// sessions, drops orphan sessions (in JSON but not in windowMap) and
// cleans up stale env entries (in windowMap but not in JSON).
func (r *Runtime) ReconcileOrphans() {
	// Find sessions without a window entry (window died during downtime).
	for id := range r.state.Sessions {
		if _, ok := r.windowMap[id]; !ok {
			slog.Warn("bootstrap: dropping orphan session (no window)", "id", id)
			delete(r.state.Sessions, id)
		}
	}

	// Find windowMap entries without a matching session (stale env).
	for sessID, idx := range r.windowMap {
		if _, ok := r.state.Sessions[sessID]; !ok {
			slog.Warn("bootstrap: removing stale window env", "session", sessID, "index", idx)
			delete(r.windowMap, sessID)
			_ = r.cfg.Tmux.UnsetEnv(windowEnvKey(idx))
		}
	}

	if len(r.state.Sessions) > 0 {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after reconcile failed", "err", err)
		}
	}
}

// DeactivateBeforeExit swaps the active session back to its own window
// so pane 0.0 shows the main TUI when the coordinator re-attaches.
// Called immediately before coordinator exits (before cancel()).
func (r *Runtime) DeactivateBeforeExit() {
	if r.activeSession == "" {
		return
	}
	r.deactivateSession()
	slog.Info("bootstrap: deactivated session before exit")
}

// RecreateAll spawns fresh tmux windows for every session in r.state.
// Used during cold-start (the tmux session was just created and
// contains no roost windows yet). Populates r.windowMap.
func (r *Runtime) RecreateAll() error {
	var dead []state.SessionID
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
		name := windowName(sess.Project, string(id))
		tmuxCmd := "exec " + spawnCmd
		if isShellCommand(sess.Command) {
			tmuxCmd = ""
		}
		target, _, err := r.cfg.Tmux.SpawnWindow(
			name, tmuxCmd, startDir,
			map[string]string{"ROOST_SESSION_ID": string(id)},
		)
		if err != nil {
			slog.Error("bootstrap: spawn failed", "id", id, "err", err)
			dead = append(dead, id)
			continue
		}
		r.windowMap[id] = target
		envKey := windowEnvKey(target)
		if err := r.cfg.Tmux.SetEnv(envKey, string(id)); err != nil {
			slog.Warn("bootstrap: set window env failed", "key", envKey, "err", err)
		}
	}
	for _, id := range dead {
		delete(r.state.Sessions, id)
	}
	if len(dead) > 0 {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after recreate failed", "err", err)
		}
	}
	return nil
}

// SetAliases sets the command alias map on state. Called once at
// startup from main.go with the config's [session] aliases.
func (r *Runtime) SetAliases(aliases map[string]string) {
	r.state.Aliases = aliases
}

// SetDefaultCommand sets the fallback command for sessions created
// without an explicit command. Called once at startup from main.go.
func (r *Runtime) SetDefaultCommand(cmd string) {
	r.state.DefaultCommand = cmd
}

// SetSyncCallbacks installs the optional tmux sync callbacks.
// Kept as a stable hook for future use.
func (r *Runtime) SetSyncCallbacks(active, status func(string)) {
	// Reserved.
}

// decodePersistedState parses the JSON-encoded persisted state blob.
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

