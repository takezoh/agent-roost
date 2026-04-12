package runtime

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	roostgit "github.com/takezoh/agent-roost/lib/git"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

// execute is the side-effect interpreter. Each Effect type has a
// dedicated case that performs the I/O on the appropriate backend.
// Effects that produce events back into the loop (tmux spawn, pane
// alive, etc.) call r.Enqueue, which is non-blocking and goroutine-
// safe so the case can fire from inside the event loop without
// risking deadlock on the channel.
func (r *Runtime) execute(eff state.Effect) {
	switch e := eff.(type) {

	// === tmux ops ===

	case state.EffSpawnTmuxWindow:
		go r.spawnTmuxWindowAsync(e)

	case state.EffKillSessionWindow:
		target, ok := r.windowMap[e.SessionID]
		if !ok {
			slog.Debug("runtime: kill window — no mapping for session", "session", e.SessionID)
			break
		}
		if err := r.cfg.Tmux.KillWindow(target); err != nil {
			slog.Error("runtime: kill window failed", "target", target, "err", err)
		}

	case state.EffActivateSession:
		r.activateSession(e.SessionID, e.Reason)

	case state.EffDeactivateSession:
		r.deactivateSession()

	case state.EffRegisterWindow:
		r.windowMap[e.SessionID] = e.WindowTarget
		envKey := windowEnvKey(e.WindowTarget)
		if err := r.cfg.Tmux.SetEnv(envKey, string(e.SessionID)); err != nil {
			slog.Debug("runtime: set window env failed", "key", envKey, "err", err)
		}

	case state.EffUnregisterWindow:
		target, ok := r.windowMap[e.SessionID]
		if ok {
			delete(r.windowMap, e.SessionID)
			envKey := windowEnvKey(target)
			if err := r.cfg.Tmux.UnsetEnv(envKey); err != nil {
				slog.Debug("runtime: unset window env failed", "key", envKey, "err", err)
			}
		}

	case state.EffSelectPane:
		target := substitutePlaceholdersString(e.Target, r.cfg.SessionName, r.cfg.RoostExe)
		if err := r.cfg.Tmux.SelectPane(target); err != nil {
			slog.Error("runtime: select pane failed", "target", target, "err", err)
		}

	case state.EffSyncStatusLine:
		// Empty line means "look up the active session's view and
		// flush its StatusLine". Non-empty means "use this exact
		// string". This indirection lets reducers schedule a status
		// line refresh without depending on the proto-side
		// SessionInfo materialization.
		line := e.Line
		if line == "" {
			line = r.activeStatusLine()
		}
		if err := r.cfg.Tmux.SetStatusLine(line); err != nil {
			slog.Debug("runtime: set status line failed", "err", err)
		}

	case state.EffSetTmuxEnv:
		if err := r.cfg.Tmux.SetEnv(e.Key, e.Value); err != nil {
			slog.Debug("runtime: set env failed", "key", e.Key, "err", err)
		}

	case state.EffUnsetTmuxEnv:
		if err := r.cfg.Tmux.UnsetEnv(e.Key); err != nil {
			slog.Debug("runtime: unset env failed", "key", e.Key, "err", err)
		}

	case state.EffCheckPaneAlive:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		alive, err := r.cfg.Tmux.PaneAlive(target)
		if err != nil {
			slog.Debug("runtime: pane-alive check failed", "pane", target, "err", err)
			return
		}
		if !alive {
			ev := state.EvPaneDied{Pane: e.Pane}
			// For pane 0.0: identify which session owns the dead pane
			// by querying its pane_id and matching against state.
			if e.Pane == "{sessionName}:0.0" {
				ev.OwnerSessionID = r.findPaneOwner(target)
			}
			r.Enqueue(ev)
		}

	case state.EffRespawnPane:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		cmd := substitutePlaceholdersString(e.Cmd, r.cfg.SessionName, r.cfg.RoostExe)
		if err := r.cfg.Tmux.RespawnPane(target, cmd); err != nil {
			slog.Error("runtime: respawn-pane failed", "pane", target, "err", err)
		}

	case state.EffDetachClient:
		// Delay so the preceding response has time to reach the client
		// before the tmux detach severs the connection.
		time.Sleep(50 * time.Millisecond)
		if err := r.cfg.Tmux.DetachClient(); err != nil {
			slog.Error("runtime: detach failed", "err", err)
		}

	case state.EffDisplayPopup:
		cmd := buildPaletteCmd(r.cfg.RoostExe, e.Tool, e.Args)
		if err := r.cfg.Tmux.DisplayPopup(e.Width, e.Height, cmd); err != nil {
			slog.Error("runtime: display-popup failed", "err", err)
		}

	case state.EffKillSession:
		if err := r.cfg.Tmux.KillSession(); err != nil {
			slog.Error("runtime: kill session failed", "err", err)
		}

	// === IPC (filled in Phase 5) ===

	case state.EffSendResponse:
		r.sendResponse(e)
	case state.EffSendResponseSync:
		r.sendResponseSync(e)
	case state.EffSendError:
		r.sendError(e)
	case state.EffBroadcastSessionsChanged:
		r.broadcastSessionsChanged(e.IsPreview)
	case state.EffBroadcastEvent:
		r.broadcastGenericEvent(e)
	case state.EffCloseConn:
		r.closeConn(e.ConnID)

	// === Persistence ===

	case state.EffPersistSnapshot:
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("runtime: persist failed", "err", err)
		}

	// === Reconciliation ===

	case state.EffReconcileWindows:
		r.reconcileWindows()

	// === fsnotify ===

	case state.EffWatchFile:
		if err := r.cfg.Watcher.Watch(e.SessionID, e.Path); err != nil {
			slog.Debug("runtime: watch failed", "path", e.Path, "err", err)
		}
		if r.relay != nil {
			r.relay.WatchFile(e.SessionID, e.Path, e.Kind)
		}

	case state.EffUnwatchFile:
		if err := r.cfg.Watcher.Unwatch(e.SessionID); err != nil {
			slog.Debug("runtime: unwatch failed", "session", e.SessionID, "err", err)
		}
		if r.relay != nil {
			r.relay.UnwatchFile(e.SessionID)
		}

	// === Event log ===

	case state.EffEventLogAppend:
		if err := r.cfg.EventLog.Append(e.SessionID, e.Line); err != nil {
			slog.Debug("runtime: event log append failed", "session", e.SessionID, "err", err)
		}

	case state.EffRemoveManagedWorktree:
		if err := roostgit.RemoveWorktree(e.Path); err != nil {
			slog.Warn("runtime: remove managed worktree failed", "path", e.Path, "err", err)
		}

	// === Async work ===

	case state.EffStartJob:
		r.submitJob(e)

	default:
		slog.Warn("runtime: unhandled effect type", "type", fmt.Sprintf("%T", eff))
	}
}

// spawnTmuxWindowAsync runs a tmux new-window in a goroutine so the
// event loop is not blocked on subprocess wait time. Posts back via
// EvTmuxWindowSpawned / EvTmuxSpawnFailed.
func (r *Runtime) spawnTmuxWindowAsync(e state.EffSpawnTmuxWindow) {
	name := windowName(e.Project, string(e.SessionID))
	spawnCmd := "exec " + e.Command
	if isShellCommand(e.Command) {
		spawnCmd = ""
	}
	target, _, err := r.cfg.Tmux.SpawnWindow(name, spawnCmd, e.StartDir, e.Env)
	if err != nil {
		r.Enqueue(state.EvTmuxSpawnFailed{
			SessionID:  e.SessionID,
			Err:        err.Error(),
			ReplyConn:  e.ReplyConn,
			ReplyReqID: e.ReplyReqID,
		})
		return
	}
	r.Enqueue(state.EvTmuxWindowSpawned{
		SessionID:    e.SessionID,
		WindowTarget: target,
		ReplyConn:    e.ReplyConn,
		ReplyReqID:   e.ReplyReqID,
	})
}

// windowName builds a stable display name for a new tmux window from
// project + session id (matches the legacy SessionService format).
func windowName(project, sessionID string) string {
	if i := strings.LastIndex(project, "/"); i >= 0 {
		project = project[i+1:]
	}
	if project == "" {
		project = "session"
	}
	return project + ":" + sessionID
}

// substitutePlaceholders replaces {sessionName} / {roostExe} tokens in
// every arg of every chain op. Used by EffSwapPane.
func substitutePlaceholders(ops [][]string, sessionName, roostExe string) [][]string {
	if len(ops) == 0 {
		return ops
	}
	out := make([][]string, len(ops))
	for i, op := range ops {
		row := make([]string, len(op))
		for j, arg := range op {
			row[j] = substitutePlaceholdersString(arg, sessionName, roostExe)
		}
		out[i] = row
	}
	return out
}

func substitutePlaceholdersString(s, sessionName, roostExe string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "{sessionName}", sessionName)
	s = strings.ReplaceAll(s, "{roostExe}", roostExe)
	return s
}

// buildPaletteCmd constructs the display-popup command string for a
// palette tool invocation. Tool name and arg values are single-quoted
// to prevent shell injection — the only way user input reaches a
// shell is through this function.
func buildPaletteCmd(roostExe, tool string, args map[string]string) string {
	cmd := shellQuote(roostExe) + " --tui palette --tool=" + shellQuote(tool)
	for k, v := range args {
		if v == "" {
			continue
		}
		cmd += " --arg=" + shellQuote(k+"="+v)
	}
	return cmd
}

// isShellCommand returns true if the command should be spawned as a
// login shell (i.e. tmux new-window with no command argument).
func isShellCommand(command string) bool {
	return command == "shell"
}

// shellQuote wraps s in single quotes and escapes inner single quotes
// with the standard POSIX '\” sequence.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// submitJob dispatches an EffStartJob to the worker pool via the
// global runner registry.
func (r *Runtime) submitJob(e state.EffStartJob) {
	worker.Dispatch(r.workers, e.JobID, e.Input)
}

// snapshotSessions converts the current state.Sessions map into the
// on-disk snapshot format. Driver bag is filled by calling each
// driver's Persist method.
func (r *Runtime) snapshotSessions() []SessionSnapshot {
	out := make([]SessionSnapshot, 0, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		drv := state.GetDriver(sess.Command)
		var bag map[string]string
		if drv != nil {
			bag = drv.Persist(sess.Driver)
		}
		var driverName string
		if drv != nil {
			driverName = drv.Name()
		}
		out = append(out, SessionSnapshot{
			ID:          string(sess.ID),
			Project:     sess.Project,
			Command:     sess.Command,
			CreatedAt:   sess.CreatedAt.UTC().Format(time.RFC3339),
			Driver:      driverName,
			DriverState: bag,
		})
	}
	return out
}

// activeStatusLine returns the View().StatusLine of whichever session
// is currently swapped into pane 0.0. Empty when no session is active
// or no driver matches.
func (r *Runtime) activeStatusLine() string {
	if r.activeSession == "" {
		return ""
	}
	sess, ok := r.state.Sessions[r.activeSession]
	if !ok {
		return ""
	}
	drv := state.GetDriver(sess.Command)
	if drv == nil {
		return ""
	}
	return drv.View(sess.Driver).StatusLine
}

// reconcileWindows compares the runtime windowMap with live tmux windows.
// Sessions whose windows have vanished are reported via EvTmuxWindowVanished.
// Orphaned windows (in windowMap but not alive in tmux) are cleaned up.
func (r *Runtime) reconcileWindows() {
	liveIndexes, err := r.listWindowIndexes()
	if err != nil {
		slog.Debug("runtime: reconcile list-windows failed", "err", err)
		return
	}
	live := make(map[string]struct{}, len(liveIndexes))
	for _, idx := range liveIndexes {
		live[idx] = struct{}{}
	}

	for sessID, target := range r.windowMap {
		if _, ok := live[target]; !ok {
			r.Enqueue(state.EvTmuxWindowVanished{SessionID: sessID})
		}
	}
}

// findPaneOwner returns the SessionID currently active (swapped into
// pane 0.0). When pane 0.0 is dead, the active session's agent exited.
func (r *Runtime) findPaneOwner(_ string) state.SessionID {
	return r.activeSession
}

// activateSession swaps a session's agent pane into pane 0.0. If another
// session is currently active it is swapped back to its own window first.
func (r *Runtime) activateSession(sessID state.SessionID, reason string) {
	target, ok := r.windowMap[sessID]
	if !ok {
		slog.Warn("runtime: activate session — no window target", "session", sessID)
		return
	}
	pane0 := r.cfg.SessionName + ":0.0"
	r.logPaneSnapshot(reason, "before-main", pane0)
	r.logPaneSnapshot(reason, "before-target", r.cfg.SessionName+":"+target+".0")
	// Swap previous active back first (may fail if window is dead; that's ok).
	if r.activeSession != "" && r.activeSession != sessID {
		prev, hasPrev := r.windowMap[r.activeSession]
		if hasPrev {
			op := []string{"swap-pane", "-d", "-s", pane0, "-t", prev + ".0"}
			if err := r.cfg.Tmux.RunChain(op); err != nil {
				slog.Warn("runtime: swap-pane back failed", "prev", prev, "err", err)
			}
		}
	}
	// Bring target into pane 0.0.
	r.activeSession = sessID
	op := []string{"swap-pane", "-d", "-s", pane0, "-t", target + ".0"}
	if err := r.cfg.Tmux.RunChain(op); err != nil {
		slog.Warn("runtime: swap-pane in failed", "target", target, "err", err)
	}
	r.logPaneSnapshot(reason, "after-main", pane0)
}

// deactivateSession swaps the current active session back to its window,
// leaving pane 0.0 showing the main TUI.
func (r *Runtime) deactivateSession() {
	if r.activeSession == "" {
		return
	}
	prev, ok := r.windowMap[r.activeSession]
	if ok {
		pane0 := r.cfg.SessionName + ":0.0"
		op := []string{"swap-pane", "-d", "-s", pane0, "-t", prev + ".0"}
		if err := r.cfg.Tmux.RunChain(op); err != nil {
			slog.Warn("runtime: deactivate swap-pane failed", "prev", prev, "err", err)
		}
	}
	r.activeSession = ""
}

// listWindowIndexes returns the live window indexes (e.g. ["0","1","2"])
// from the configured tmux backend.
func (r *Runtime) listWindowIndexes() ([]string, error) {
	type indexLister interface {
		ListWindowIndexes() ([]string, error)
	}
	if l, ok := r.cfg.Tmux.(indexLister); ok {
		return l.ListWindowIndexes()
	}
	return nil, nil
}

// windowEnvKey returns the tmux session-level env var name for a window index.
func windowEnvKey(windowIndex string) string {
	return "ROOST_W_" + windowIndex
}
