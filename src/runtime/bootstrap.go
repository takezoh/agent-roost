package runtime

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// Bootstrap helpers used at startup before the event loop starts.
// These mutate r.state and r.sessionPanes directly — safe because no
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

// LoadSessionPanes reads the ROOST_SESSION_* tmux session environment
// variables and populates r.sessionPanes. Called on warm start after
// LoadSnapshot.
func (r *Runtime) LoadSessionPanes() error {
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
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if !strings.HasPrefix(parts[0], "ROOST_SESSION_") {
			continue
		}
		sessID := state.SessionID(strings.TrimPrefix(parts[0], "ROOST_SESSION_"))
		r.sessionPanes[sessID] = parts[1]
	}
	return nil
}

// ReconcileOrphans compares the loaded sessionPanes against the snapshot
// sessions, drops orphan sessions (in JSON but not in sessionPanes) and
// cleans up stale env entries (in windowMap but not in JSON).
func (r *Runtime) ReconcileOrphans() {
	// Find sessions without a pane entry (pane died during downtime).
	for id := range r.state.Sessions {
		if _, ok := r.sessionPanes[id]; !ok {
			slog.Warn("bootstrap: dropping orphan session (no pane)", "id", id)
			delete(r.state.Sessions, id)
		}
	}

	// Find sessionPanes entries without a matching session (stale env).
	for sessID := range r.sessionPanes {
		if sessID == "_main" {
			continue
		}
		if _, ok := r.state.Sessions[sessID]; !ok {
			delete(r.sessionPanes, sessID)
			slog.Warn("bootstrap: removing stale pane env", "session", sessID)
			_ = r.cfg.Tmux.UnsetEnv(sessionPaneEnvKey(sessID))
		}
	}

	if len(r.state.Sessions) > 0 {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after reconcile failed", "err", err)
		}
	}
}

// RecoverActivePaneAtMain restores a consistent main-pane owner on warm start.
func (r *Runtime) RecoverActivePaneAtMain() {
	paneAtZero, err := r.cfg.Tmux.PaneID(r.mainPaneTarget())
	if err != nil {
		slog.Debug("bootstrap: could not get pane id at 0.0", "err", err)
		return
	}
	if paneAtZero == "" {
		return
	}
	var owner state.SessionID
	for id, paneID := range r.sessionPanes {
		if id == "_main" || paneID != paneAtZero {
			continue
		}
		owner = id
		break
	}
	if owner == "" {
		if r.sessionPanes["_main"] == "" {
			r.sessionPanes["_main"] = paneAtZero
			_ = r.cfg.Tmux.SetEnv("ROOST_SESSION__main", paneAtZero)
		}
		r.activeSession = ""
		slog.Info("bootstrap: main TUI active at 0.0", "pane", paneAtZero)
		return
	}
	if r.sessionPanes["_main"] == "" {
		r.activeSession = owner
		slog.Warn("bootstrap: main pane id missing; leaving active session in place", "session", owner, "pane", paneAtZero)
		return
	}
	r.activeSession = owner
	slog.Info("bootstrap: session left active at 0.0; restoring main TUI", "session", owner, "pane", paneAtZero, "main_pane", r.sessionPanes["_main"])
	if !r.swapMainIntoMain() {
		slog.Warn("bootstrap: failed to restore main TUI at 0.0", "session", owner)
		return
	}
	r.activeSession = ""
}

func (r *Runtime) RecoverWarmStartSessions() {
	now := time.Now()
	changed := false
	for sessID, sess := range r.state.Sessions {
		drv := state.GetDriver(sess.Command)
		if drv == nil {
			continue
		}
		recoverer, ok := drv.(state.WarmStartRecoverer)
		if !ok {
			continue
		}
		next, effs := recoverer.WarmStartRecover(sess.Driver, now)
		sess.Driver = next
		r.state.Sessions[sessID] = sess
		for _, eff := range effs {
			r.execute(r.bootstrapSessionEffect(sessID, now, eff))
		}
		changed = true
	}
	if changed {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after warm start recovery failed", "err", err)
		}
	}
}

func (r *Runtime) bootstrapSessionEffect(sessID state.SessionID, now time.Time, eff state.Effect) state.Effect {
	switch e := eff.(type) {
	case state.EffStartJob:
		r.state.NextJobID++
		jobID := r.state.NextJobID
		r.state.Jobs[jobID] = state.JobMeta{
			SessionID: sessID,
			StartedAt: now,
		}
		e.JobID = jobID
		return e
	case state.EffEventLogAppend:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e
	case state.EffWatchFile:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e
	case state.EffUnwatchFile:
		if e.SessionID == "" {
			e.SessionID = sessID
		}
		return e
	default:
		return eff
	}
}

// DeactivateBeforeExit moves the active session back to its own window
// so pane 0.0 shows the main TUI when the coordinator re-attaches.
func (r *Runtime) DeactivateBeforeExit() {
	if r.activeSession == "" {
		return
	}
	r.deactivateSession()
	slog.Info("bootstrap: deactivated session before exit")
}

// RecreateAll spawns fresh tmux windows for every session in r.state.
// Used during cold-start (the tmux session was just created and
// contains no roost windows yet). Populates r.sessionPanes.
func (r *Runtime) RecreateAll() error {
	size := r.mainPaneSize()
	var dead []state.SessionID
	for id, sess := range r.state.Sessions {
		drv := state.GetDriver(sess.Command)
		if drv == nil {
			continue
		}
		launch, err := drv.PrepareLaunch(sess.Driver, state.LaunchModeColdStart, sess.Project, sess.Command)
		if err != nil {
			slog.Error("bootstrap: prepare launch failed", "id", id, "err", err)
			dead = append(dead, id)
			continue
		}
		name := windowName(sess.Project, string(id))
		tmuxCmd := "exec " + launch.Command
		if isShellCommand(launch.Command) {
			tmuxCmd = ""
		}
		target, paneID, err := r.cfg.Tmux.SpawnWindow(
			name, tmuxCmd, launch.StartDir,
			map[string]string{"ROOST_SESSION_ID": string(id)},
		)
		if err != nil {
			slog.Error("bootstrap: spawn failed", "id", id, "err", err)
			dead = append(dead, id)
			continue
		}
		r.resizeWindowToMain(r.cfg.SessionName+":"+target, size)
		r.sessionPanes[id] = paneID
		envKey := sessionPaneEnvKey(id)
		if err := r.cfg.Tmux.SetEnv(envKey, paneID); err != nil {
			slog.Warn("bootstrap: set pane env failed", "key", envKey, "err", err)
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

// RespawnMainPane runs respawn-pane for the main TUI.
func (r *Runtime) RespawnMainPane() {
	target := r.sessionPanes["_main"]
	if target == "" {
		target = r.mainPaneTarget()
	}

	// Double check to protect active sessions if mapping failed
	if target == r.mainPaneTarget() && r.activeSession != "" {
		slog.Warn("bootstrap: skipping main TUI respawn to protect active session at 0.0")
		return
	}

	slog.Info("bootstrap: respawning main TUI", "target", target)
	_ = r.cfg.Tmux.RespawnPane(target, r.cfg.RoostExe+" --tui main")
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
