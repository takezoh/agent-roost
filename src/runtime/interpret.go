package runtime

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

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

	case state.EffKillTmuxWindow:
		if err := r.cfg.Tmux.KillWindow(string(e.WindowID)); err != nil {
			slog.Error("runtime: kill window failed", "window", e.WindowID, "err", err)
		}

	case state.EffSwapPane:
		ops := substitutePlaceholders(e.ChainOps, r.cfg.SessionName, r.cfg.RoostExe)
		// Execute each swap-pane independently. The old active may be
		// a dead window (agent exited, remain-on-exit off); if the
		// first swap (return old active) fails, the second swap
		// (bring in target) must still run. RunChain uses ";" which
		// aborts the entire chain on the first failure.
		for _, op := range ops {
			if err := r.cfg.Tmux.RunChain(op); err != nil {
				slog.Warn("runtime: swap-pane step failed", "op", op, "err", err)
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
	wid, paneID, err := r.cfg.Tmux.SpawnWindow(name, spawnCmd, e.StartDir, e.Env)
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
		SessionID:   e.SessionID,
		WindowID:    state.WindowID(wid),
		PaneID: paneID,
		ReplyConn:   e.ReplyConn,
		ReplyReqID:  e.ReplyReqID,
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

// shellQuote wraps s in single quotes with internal single quotes
// escaped as '\''. This is the POSIX-portable way to prevent shell
// interpretation of any character in s.
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
			WindowID:    string(sess.WindowID),
			PaneID: sess.PaneID,
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
	if r.state.Active == "" {
		return ""
	}
	for _, sess := range r.state.Sessions {
		if sess.WindowID != r.state.Active {
			continue
		}
		drv := state.GetDriver(sess.Command)
		if drv == nil {
			return ""
		}
		return drv.View(sess.Driver).StatusLine
	}
	return ""
}

// reconcileWindows compares live tmux windows with state sessions.
// Sessions whose windows have vanished are reported via
// EvTmuxWindowVanished so the reducer evicts them. Orphaned windows
// (carrying @roost_id but not tracked in state.Sessions) are killed.
func (r *Runtime) reconcileWindows() {
	list, err := r.listRoostWindows()
	if err != nil {
		slog.Debug("runtime: reconcile list-windows failed", "err", err)
		return
	}
	live := make(map[string]struct{}, len(list))
	for _, w := range list {
		live[w.WindowID] = struct{}{}
	}
	for _, sess := range r.state.Sessions {
		if sess.WindowID == "" {
			continue
		}
		if _, ok := live[string(sess.WindowID)]; !ok {
			r.Enqueue(state.EvTmuxWindowVanished{WindowID: sess.WindowID})
		}
	}

	// Reverse check: kill orphaned windows whose @roost_id has no
	// matching session in state. Window 0 (TUI panes) never carries
	// @roost_id, so it is never affected.
	tracked := make(map[string]struct{}, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		tracked[string(sess.ID)] = struct{}{}
	}
	for _, w := range list {
		if _, ok := tracked[w.ID]; !ok {
			slog.Info("runtime: killing orphaned window", "window", w.WindowID, "roost_id", w.ID)
			if err := r.cfg.Tmux.KillWindow(w.WindowID); err != nil {
				slog.Warn("runtime: kill orphaned window failed", "window", w.WindowID, "err", err)
			}
		}
	}
}

// findPaneOwner queries the pane_id of a tmux pane and maps it back
// to the owning session via PaneID. Used when pane 0.0 is dead to
// find which session's agent process exited.
func (r *Runtime) findPaneOwner(paneTarget string) state.SessionID {
	type displayer interface {
		DisplayMessage(target, format string) (string, error)
	}
	d, ok := r.cfg.Tmux.(displayer)
	if !ok {
		return ""
	}
	out, err := d.DisplayMessage(paneTarget, "#{pane_id}")
	if err != nil {
		return ""
	}
	paneID := strings.TrimSpace(out)
	if paneID == "" {
		return ""
	}
	for _, sess := range r.state.Sessions {
		if sess.PaneID == paneID {
			return sess.ID
		}
	}
	return ""
}

