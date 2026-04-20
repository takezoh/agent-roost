package runtime

import (
	"log/slog"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/agent-roost/uiproc"
)

// Bootstrap helpers used at startup before the event loop starts.
// These mutate r.state and r.sessionPanes directly — safe because no
// goroutine is reading state yet.

// LoadSnapshot reads sessions.json and registers each session in
// r.state. Driver state is restored via the registered Driver's
// Restore method.
func (r *Runtime) LoadSnapshot(coldStart bool) error {
	snaps, err := r.cfg.Persist.Load()
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return nil
	}
	now := time.Now()
	for _, snap := range snaps {
		createdAt, _ := time.Parse(time.RFC3339, snap.CreatedAt)
		if createdAt.IsZero() {
			createdAt = now
		}
		sess := state.Session{ID: state.SessionID(snap.ID), Project: snap.Project, CreatedAt: createdAt}
		for _, fsnap := range snap.Frames {
			drv := state.GetDriver(fsnap.Command)
			if drv == nil {
				slog.Warn("bootstrap: no driver for command, skipping frame", "command", fsnap.Command)
				break
			}
			if coldStart && fsnap.DriverState["status"] == "running" {
				fsnap.DriverState["status"] = "waiting"
			}
			frameCreatedAt, _ := time.Parse(time.RFC3339, fsnap.CreatedAt)
			if frameCreatedAt.IsZero() {
				frameCreatedAt = createdAt
			}
			sess.Frames = append(sess.Frames, state.SessionFrame{
				ID:            state.FrameID(fsnap.ID),
				Project:       fsnap.Project,
				Command:       fsnap.Command,
				LaunchOptions: fsnap.LaunchOptions,
				CreatedAt:     frameCreatedAt,
				Driver:        drv.Restore(fsnap.DriverState, now),
			})
		}
		if len(sess.Frames) > 0 {
			sess.Command = sess.Frames[0].Command
			sess.LaunchOptions = sess.Frames[0].LaunchOptions
			sess.Driver = sess.Frames[0].Driver
			r.state.Sessions[sess.ID] = sess
		}
	}
	slog.Info("bootstrap: snapshot loaded", "count", len(snaps))
	return nil
}

// LoadSessionPanes reads the ROOST_FRAME_* tmux session environment
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
		if !strings.HasPrefix(parts[0], "ROOST_FRAME_") {
			continue
		}
		frameID := state.FrameID(strings.TrimPrefix(parts[0], "ROOST_FRAME_"))
		r.sessionPanes[frameID] = parts[1]
	}
	return nil
}

// ReconcileOrphans compares the loaded sessionPanes against the snapshot
// sessions, drops orphan sessions (in JSON but not in sessionPanes) and
// cleans up stale env entries (in windowMap but not in JSON).
func (r *Runtime) ReconcileOrphans() {
	for id, sess := range r.state.Sessions {
		cut := len(sess.Frames)
		for i, frame := range sess.Frames {
			if _, ok := r.sessionPanes[frame.ID]; !ok {
				cut = i
				break
			}
		}
		if cut == len(sess.Frames) {
			continue
		}
		if cut == 0 {
			slog.Warn("bootstrap: dropping orphan session (missing root frame)", "id", id)
			delete(r.state.Sessions, id)
			continue
		}
		sess.Frames = append([]state.SessionFrame(nil), sess.Frames[:cut]...)
		r.state.Sessions[id] = sess
	}

	// Find sessionPanes entries without a matching frame (stale env).
	for frameID := range r.sessionPanes {
		if frameID == "_main" {
			continue
		}
		found := false
		for _, sess := range r.state.Sessions {
			for _, frame := range sess.Frames {
				if frame.ID == frameID {
					found = true
					break
				}
			}
		}
		if !found {
			delete(r.sessionPanes, frameID)
			slog.Warn("bootstrap: removing stale pane env", "frame", frameID)
			_ = r.cfg.Tmux.UnsetEnv(sessionPaneEnvKey(frameID))
		}
	}

	if len(r.state.Sessions) > 0 {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after reconcile failed", "err", err)
		}
	}
}

// RecoverActivePaneAtMain restores a consistent main-pane owner on warm start.
func (r *Runtime) RecoverActivePaneAtMain() { //nolint:funlen
	paneAtZero, err := r.cfg.Tmux.PaneID(r.mainPaneTarget())
	if err != nil {
		slog.Debug("bootstrap: could not get pane id at 0.0", "err", err)
		return
	}
	if paneAtZero == "" {
		return
	}
	var owner state.FrameID
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
			_ = r.cfg.Tmux.SetEnv("ROOST_FRAME__main", paneAtZero)
		}
		r.activeSession = ""
		slog.Info("bootstrap: main TUI active at 0.0", "pane", paneAtZero)
		return
	}
	if r.sessionPanes["_main"] == "" {
		r.activeFrameID = owner
		for sid, sess := range r.state.Sessions {
			for _, frame := range sess.Frames {
				if frame.ID == owner {
					r.activeSession = sid
				}
			}
		}
		slog.Warn("bootstrap: main pane id missing; leaving active frame in place", "frame", owner, "pane", paneAtZero)
		return
	}
	r.activeFrameID = owner
	for sid, sess := range r.state.Sessions {
		for _, frame := range sess.Frames {
			if frame.ID == owner {
				r.activeSession = sid
			}
		}
	}
	slog.Info("bootstrap: session left active at 0.0; restoring main TUI", "frame", owner, "pane", paneAtZero, "main_pane", r.sessionPanes["_main"])
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
		for i, frame := range sess.Frames {
			drv := state.GetDriver(frame.Command)
			if drv == nil {
				continue
			}
			recoverer, ok := drv.(state.WarmStartRecoverer)
			if !ok {
				continue
			}
			next, effs := recoverer.WarmStartRecover(frame.Driver, now)
			sess.Frames[i].Driver = next
			r.state.Sessions[sessID] = sess
			for _, eff := range effs {
				r.execute(r.bootstrapSessionEffect(sessID, frame.ID, now, eff))
			}
			changed = true
		}
	}
	if changed {
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("bootstrap: persist after warm start recovery failed", "err", err)
		}
	}
}

func (r *Runtime) bootstrapSessionEffect(sessID state.SessionID, frameID state.FrameID, now time.Time, eff state.Effect) state.Effect {
	switch e := eff.(type) {
	case state.EffStartJob:
		r.state.NextJobID++
		jobID := r.state.NextJobID
		r.state.Jobs[jobID] = state.JobMeta{
			SessionID: sessID,
			FrameID:   frameID,
			StartedAt: now,
		}
		e.JobID = jobID
		return e
	case state.EffEventLogAppend:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e
	case state.EffWatchFile:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e
	case state.EffUnwatchFile:
		if e.FrameID == "" {
			e.FrameID = frameID
		}
		return e
	default:
		return eff
	}
}

// deactivateBeforeExit moves the active session back to its own window
// so pane 0.0 shows the main TUI when the coordinator re-attaches.
// Called from the event loop's defer stack in Run — safe to access
// runtime state here because the select loop has already exited.
func (r *Runtime) deactivateBeforeExit() {
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
		for _, frame := range sess.Frames {
			drv := state.GetDriver(frame.Command)
			if drv == nil {
				continue
			}
			launch, err := drv.PrepareLaunch(frame.Driver, state.LaunchModeColdStart, frame.Project, frame.Command, frame.LaunchOptions)
			if err != nil {
				slog.Error("bootstrap: prepare launch failed", "id", id, "frame", frame.ID, "err", err)
				dead = append(dead, id)
				break
			}
			name := windowName(frame.Project, string(frame.ID))
			tmuxCmd := "exec " + launch.Command
			if isShellCommand(launch.Command) {
				tmuxCmd = ""
			}
			target, paneID, err := r.cfg.Tmux.SpawnWindow(
				name, tmuxCmd, launch.StartDir,
				map[string]string{"ROOST_SESSION_ID": string(id), "ROOST_FRAME_ID": string(frame.ID)},
			)
			if err != nil {
				slog.Error("bootstrap: spawn failed", "id", id, "frame", frame.ID, "err", err)
				dead = append(dead, id)
				break
			}
			r.resizeWindowToMain(r.cfg.SessionName+":"+target, size)
			r.sessionPanes[frame.ID] = paneID
			envKey := sessionPaneEnvKey(frame.ID)
			if err := r.cfg.Tmux.SetEnv(envKey, paneID); err != nil {
				slog.Warn("bootstrap: set pane env failed", "key", envKey, "err", err)
			}
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
	_ = r.cfg.Tmux.RespawnPane(target, uiproc.Main().Command(r.cfg.RoostExe))
}
