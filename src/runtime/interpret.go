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
	case state.EffSpawnTmuxWindow, state.EffKillSessionWindow, state.EffActivateSession,
		state.EffDeactivateSession, state.EffRegisterWindow, state.EffUnregisterWindow,
		state.EffSelectPane, state.EffSyncStatusLine, state.EffSetTmuxEnv,
		state.EffUnsetTmuxEnv, state.EffCheckPaneAlive, state.EffRespawnPane,
		state.EffDetachClient, state.EffDisplayPopup, state.EffKillSession,
		state.EffReconcileWindows:
		r.executeTmuxEffect(e)

	case state.EffSendResponse, state.EffSendResponseSync, state.EffSendError,
		state.EffBroadcastSessionsChanged, state.EffBroadcastEvent, state.EffCloseConn:
		r.executeIPCEffect(e)

	case state.EffPersistSnapshot:
		if err := r.cfg.Persist.Save(r.snapshotSessions()); err != nil {
			slog.Error("runtime: persist failed", "err", err)
		}

	case state.EffWatchFile, state.EffUnwatchFile:
		r.executeFSEffect(e)

	case state.EffEventLogAppend:
		if err := r.cfg.EventLog.Append(e.SessionID, e.Line); err != nil {
			slog.Debug("runtime: event log append failed", "session", e.SessionID, "err", err)
		}

	case state.EffRemoveManagedWorktree:
		if err := roostgit.RemoveWorktree(e.Path); err != nil {
			slog.Warn("runtime: remove managed worktree failed", "path", e.Path, "err", err)
		}

	case state.EffStartJob:
		r.submitJob(e)

	default:
		slog.Warn("runtime: unhandled effect type", "type", fmt.Sprintf("%T", eff))
	}
}

func (r *Runtime) executeTmuxEffect(eff state.Effect) {
	switch e := eff.(type) {
	case state.EffSpawnTmuxWindow:
		go r.spawnTmuxWindowAsync(e)

	case state.EffKillSessionWindow:
		target, ok := r.windowMap[e.SessionID]
		if ok && target != "0" {
			if err := r.cfg.Tmux.KillWindow(target); err != nil {
				slog.Error("runtime: kill window failed", "target", target, "err", err)
			}
		}

	case state.EffActivateSession:
		r.activateSession(e.SessionID, e.Reason)

	case state.EffDeactivateSession:
		r.deactivateSession()

	case state.EffRegisterWindow:
		r.windowMap[e.SessionID] = e.WindowTarget
		envKey := windowEnvKey(e.WindowTarget)
		r.cfg.Tmux.SetEnv(envKey, string(e.SessionID))

	case state.EffUnregisterWindow:
		if target, ok := r.windowMap[e.SessionID]; ok {
			delete(r.windowMap, e.SessionID)
			if target == "0" {
				r.cfg.Tmux.UnsetEnv(activeSessionEnvKey)
			} else {
				envKey := windowEnvKey(target)
				r.cfg.Tmux.UnsetEnv(envKey)
			}
		}

	case state.EffSelectPane:
		target := substitutePlaceholdersString(e.Target, r.cfg.SessionName, r.cfg.RoostExe)
		r.cfg.Tmux.SelectPane(target)

	case state.EffSyncStatusLine:
		line := e.Line
		if line == "" {
			line = r.activeStatusLine()
		}
		r.cfg.Tmux.SetStatusLine(line)

	case state.EffSetTmuxEnv:
		r.cfg.Tmux.SetEnv(e.Key, e.Value)

	case state.EffUnsetTmuxEnv:
		r.cfg.Tmux.UnsetEnv(e.Key)

	case state.EffCheckPaneAlive:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		if alive, err := r.cfg.Tmux.PaneAlive(target); err == nil && !alive {
			ev := state.EvPaneDied{Pane: e.Pane}
			if e.Pane == "{sessionName}:0.0" {
				ev.OwnerSessionID = r.findPaneOwner(target)
			}
			r.Enqueue(ev)
		}

	case state.EffRespawnPane:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		cmd := substitutePlaceholdersString(e.Cmd, r.cfg.SessionName, r.cfg.RoostExe)
		r.cfg.Tmux.RespawnPane(target, cmd)

	case state.EffDetachClient:
		time.Sleep(50 * time.Millisecond)
		r.cfg.Tmux.DetachClient()

	case state.EffDisplayPopup:
		cmd := buildPaletteCmd(r.cfg.RoostExe, e.Tool, e.Args)
		r.cfg.Tmux.DisplayPopup(e.Width, e.Height, cmd)

	case state.EffKillSession:
		r.cfg.Tmux.KillSession()

	case state.EffReconcileWindows:
		r.reconcileWindows()
	}
}

func (r *Runtime) executeIPCEffect(eff state.Effect) {
	switch e := eff.(type) {
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
	}
}

func (r *Runtime) executeFSEffect(eff state.Effect) {
	switch e := eff.(type) {
	case state.EffWatchFile:
		r.cfg.Watcher.Watch(e.SessionID, e.Path)
		if r.relay != nil {
			r.relay.WatchFile(e.SessionID, e.Path, e.Kind)
		}

	case state.EffUnwatchFile:
		r.cfg.Watcher.Unwatch(e.SessionID)
		if r.relay != nil {
			r.relay.UnwatchFile(e.SessionID)
		}
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
// is currently shown in pane 0.0. Empty when no session is active
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
// Sessions whose parked windows have vanished are reported via
// EvTmuxWindowVanished. The active session is tracked at window "0".
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

	if r.mainWindow != "" && r.mainWindow != "0" {
		if _, ok := live[r.mainWindow]; !ok {
			_ = r.cfg.Tmux.UnsetEnv(mainWindowEnvKey())
			r.mainWindow = "0"
		}
	}

	for sessID, target := range r.windowMap {
		if target == "0" {
			continue
		}
		if _, ok := live[target]; !ok {
			r.Enqueue(state.EvTmuxWindowVanished{SessionID: sessID})
		}
	}
}

// findPaneOwner returns the SessionID currently active in pane 0.0.
func (r *Runtime) findPaneOwner(_ string) state.SessionID {
	return r.activeSession
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
