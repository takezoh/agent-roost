package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	roostgit "github.com/takezoh/agent-roost/lib/git"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/agent-roost/uiproc"
)

// execute is the side-effect interpreter. Each Effect type has a
// dedicated case that performs the I/O on the appropriate backend.
// Effects that produce events back into the loop (tmux spawn, pane
// alive, etc.) call r.Enqueue, which is non-blocking and goroutine-
// safe so the case can fire from inside the event loop without
// risking deadlock on the channel.
func (r *Runtime) execute(eff state.Effect) { //nolint:funlen
	switch e := eff.(type) {
	case state.EffSpawnTmuxWindow, state.EffKillSessionWindow, state.EffActivateSession,
		state.EffDeactivateSession, state.EffRegisterPane, state.EffUnregisterPane,
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
		if err := r.cfg.EventLog.Append(e.FrameID, e.Line); err != nil {
			slog.Debug("runtime: event log append failed", "frame", e.FrameID, "err", err)
		}

	case state.EffToolLogAppend:
		if err := r.cfg.ToolLog.Append(e.Namespace, e.Project, e.Line); err != nil {
			slog.Debug("runtime: tool log append failed",
				"namespace", e.Namespace, "project", e.Project, "err", err)
		}

	case state.EffRemoveManagedWorktree:
		path := e.Path
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := roostgit.RemoveWorktree(ctx, path); err != nil {
				slog.Warn("runtime: remove managed worktree failed", "path", path, "err", err)
			}
		}()

	case state.EffStartJob:
		r.submitJob(e)

	case state.EffNotify:
		r.cfg.Notifier.Dispatch(e)

	case state.EffRecordNotification:
		r.broadcastAgentNotification(e)
		source := fmt.Sprintf("osc%d", e.Cmd)
		if err := r.cfg.EventLog.Append(e.FrameID, oscEventLogLine(source, e.Title, e.Body)); err != nil {
			slog.Debug("runtime: osc event log failed", "frame", e.FrameID, "err", err)
		}
		r.cfg.Notifier.DispatchOSC(e.Title, e.Body, source)

	case state.EffSendTmuxKeys:
		r.executeSendTmuxKeys(e)

	case state.EffInjectPrompt:
		r.executeInjectPrompt(e)

	default:
		slog.Warn("runtime: unhandled effect type", "type", fmt.Sprintf("%T", eff))
	}
}

func (r *Runtime) executeSendTmuxKeys(e state.EffSendTmuxKeys) {
	pane := r.sessionPaneForSession(e.SessionID)
	if pane == "" {
		r.executeIPCEffect(state.EffSendError{
			ConnID:  e.ConnID,
			ReqID:   e.ReqID,
			Code:    "not_found",
			Message: "no pane registered for session: " + string(e.SessionID),
		})
		return
	}
	var err error
	if e.WithEnter {
		err = r.cfg.Tmux.SendKeys(pane, e.Text)
	} else {
		err = r.cfg.Tmux.SendKey(pane, e.Key)
	}
	if err != nil {
		slog.Warn("runtime: send-keys failed", "session", e.SessionID, "err", err)
		r.executeIPCEffect(state.EffSendError{
			ConnID:  e.ConnID,
			ReqID:   e.ReqID,
			Code:    "internal",
			Message: err.Error(),
		})
		return
	}
	r.executeIPCEffect(state.EffSendResponse{ConnID: e.ConnID, ReqID: e.ReqID, Body: nil})
}

func (r *Runtime) executeInjectPrompt(e state.EffInjectPrompt) {
	inj := NewRuntimeTmuxInjector(r.sessionPanes, r.cfg.Tmux)
	if err := InjectPrompt(inj, e.FrameID, e.Text); err != nil {
		slog.Warn("runtime: inject prompt failed", "frame", e.FrameID, "err", err)
	}
}

func (r *Runtime) executeTmuxEffect(eff state.Effect) { //nolint:funlen
	switch e := eff.(type) {
	case state.EffSpawnTmuxWindow:
		go r.spawnTmuxWindowAsync(e)

	case state.EffKillSessionWindow:
		if target := r.sessionPanes[e.FrameID]; target != "" {
			if err := r.cfg.Tmux.KillPaneWindow(target); err != nil {
				slog.Error("runtime: kill window failed", "target", target, "err", err)
			}
		}

	case state.EffActivateSession:
		r.activateSession(e.SessionID, e.Reason)

	case state.EffDeactivateSession:
		r.deactivateSession()

	case state.EffRegisterPane:
		r.sessionPanes[e.FrameID] = e.PaneTarget
		_ = r.cfg.Tmux.SetEnv(sessionPaneEnvKey(e.FrameID), e.PaneTarget)
		if r.taps != nil {
			r.taps.start(e.FrameID, e.PaneTarget, r.Enqueue)
		}

	case state.EffUnregisterPane:
		if target, ok := r.sessionPanes[e.FrameID]; ok {
			if r.taps != nil {
				r.taps.stop(e.FrameID)
			}
			delete(r.sessionPanes, e.FrameID)
			delete(r.parkedPaneSnapshot, e.FrameID)
			_ = r.cfg.Tmux.UnsetEnv(sessionPaneEnvKey(e.FrameID))
			r.cfg.EventLog.Close(e.FrameID)
			if r.cfg.TerminalEvict != nil {
				r.cfg.TerminalEvict(target)
			}
		}

	case state.EffSelectPane:
		target := substitutePlaceholdersString(e.Target, r.cfg.SessionName, r.cfg.RoostExe)
		_ = r.cfg.Tmux.SelectPane(target)

	case state.EffSyncStatusLine:
		line := e.Line
		if line == "" {
			line = r.activeStatusLine()
		}
		_ = r.cfg.Tmux.SetStatusLine(line)

	case state.EffSetTmuxEnv:
		_ = r.cfg.Tmux.SetEnv(e.Key, e.Value)

	case state.EffUnsetTmuxEnv:
		_ = r.cfg.Tmux.UnsetEnv(e.Key)

	case state.EffCheckPaneAlive:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		if alive, err := r.cfg.Tmux.PaneAlive(target); err == nil && !alive {
			ev := state.EvPaneDied{Pane: e.Pane}
			if e.Pane == "{sessionName}:0.0" {
				ev.OwnerFrameID = r.findPaneOwner(target)
			}
			r.Enqueue(ev)
		}

	case state.EffRespawnPane:
		target := substitutePlaceholdersString(e.Pane, r.cfg.SessionName, r.cfg.RoostExe)
		_ = r.cfg.Tmux.RespawnPane(target, e.Proc.Command(r.cfg.RoostExe))

	case state.EffDetachClient:
		_ = r.cfg.Tmux.DetachClient()

	case state.EffDisplayPopup:
		_ = r.cfg.Tmux.DisplayPopup(e.Width, e.Height, uiproc.Palette(e.Tool, e.Args).Command(r.cfg.RoostExe))

	case state.EffKillSession:
		_ = r.cfg.Tmux.KillSession()

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
		_ = r.cfg.Watcher.Watch(e.FrameID, e.Path)
		if r.relay != nil {
			r.relay.WatchFile(e.FrameID, e.Path, e.Kind)
		}

	case state.EffUnwatchFile:
		_ = r.cfg.Watcher.Unwatch(e.FrameID)
		if r.relay != nil {
			r.relay.UnwatchFile(e.FrameID)
		}
	}
}

// spawnTmuxWindowAsync runs a tmux new-window in a goroutine so the
// event loop is not blocked on subprocess wait time. Posts back via
// EvTmuxPaneSpawned / EvTmuxSpawnFailed.
func (r *Runtime) spawnTmuxWindowAsync(e state.EffSpawnTmuxWindow) {
	name := windowName(e.Project, string(e.FrameID))
	spawnCmd := "exec " + e.Command
	if isShellCommand(e.Command) {
		spawnCmd = ""
	} else if len(e.Stdin) > 0 {
		spawnCmd = wrapCommandWithStdin(e.Command, e.Stdin)
	}
	size := r.mainPaneSize()
	target, paneID, err := r.cfg.Tmux.SpawnWindow(name, spawnCmd, e.StartDir, e.Env)
	if err != nil {
		r.Enqueue(state.EvTmuxSpawnFailed{
			SessionID:  e.SessionID,
			FrameID:    e.FrameID,
			Err:        err.Error(),
			ReplyConn:  e.ReplyConn,
			ReplyReqID: e.ReplyReqID,
		})
		return
	}
	r.resizeWindowToMain(r.cfg.SessionName+":"+target, size)
	r.Enqueue(state.EvTmuxPaneSpawned{
		SessionID:  e.SessionID,
		FrameID:    e.FrameID,
		PaneTarget: paneID,
		ReplyConn:  e.ReplyConn,
		ReplyReqID: e.ReplyReqID,
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

func substitutePlaceholdersString(s, sessionName, roostExe string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "{sessionName}", sessionName)
	s = strings.ReplaceAll(s, "{roostExe}", roostExe)
	return s
}

// isShellCommand returns true if the command should be spawned as a
// login shell (i.e. tmux new-window with no command argument).
func isShellCommand(command string) bool {
	return command == "shell"
}

// wrapCommandWithStdin writes input to a temp file and returns a shell
// command that feeds the file to command on stdin, then deletes it.
func wrapCommandWithStdin(command string, input []byte) string {
	f, err := os.CreateTemp("", "roost-push-*.in")
	if err != nil {
		slog.Warn("buildStdinCommand: could not create temp file, stdin ignored",
			"err", err)
		return "exec " + command
	}
	if _, err := f.Write(input); err != nil {
		slog.Warn("buildStdinCommand: could not write temp file, stdin ignored",
			"err", err, "path", f.Name())
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "exec " + command
	}
	_ = f.Close()
	tmp := f.Name() // CreateTemp paths never contain special shell chars
	return "bash -c " + shellQuote(command+" < "+tmp+"; _ec=$?; rm -f "+tmp+"; exit $_ec")
}

// shellQuote wraps s in single quotes and escapes inner single quotes
// with the standard POSIX '\" sequence.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// submitJob dispatches an EffStartJob to the worker pool via the
// global runner registry.
func (r *Runtime) submitJob(e state.EffStartJob) {
	worker.Dispatch(r.workers, e.JobID, e.Input)
}

// oscEventLogLine formats a single EVENTS log line for an OSC notification.
// Format: "[osc9] title" / "[osc99] title | body" / "[osc777] title | body"
func oscEventLogLine(source, title, body string) string {
	if body == "" {
		return fmt.Sprintf("[%s] %s", source, title)
	}
	if title == "" {
		return fmt.Sprintf("[%s] %s", source, body)
	}
	return fmt.Sprintf("[%s] %s | %s", source, title, body)
}

// snapshotSessions converts the current state.Sessions map into the
// on-disk snapshot format. Driver bag is filled by calling each
// driver's Persist method.
func (r *Runtime) snapshotSessions() []SessionSnapshot {
	out := make([]SessionSnapshot, 0, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		frames := make([]SessionFrameSnapshot, 0, len(sess.Frames))
		for _, frame := range sess.Frames {
			drv := state.GetDriver(frame.Command)
			var bag map[string]string
			var driverName string
			if drv != nil {
				bag = drv.Persist(frame.Driver)
				driverName = drv.Name()
			}
			// Strip InitialInput before persisting: it is a one-shot spawn
			// parameter and must not be written to sessions.json (would
			// re-pipe stale stdin on cold-start recovery and leak content).
			persistOpts := frame.LaunchOptions
			persistOpts.InitialInput = nil
			frames = append(frames, SessionFrameSnapshot{
				ID:            string(frame.ID),
				Project:       frame.Project,
				Command:       frame.Command,
				LaunchOptions: persistOpts,
				CreatedAt:     frame.CreatedAt.UTC().Format(time.RFC3339),
				Driver:        driverName,
				DriverState:   bag,
			})
		}
		out = append(out, SessionSnapshot{
			ID:        string(sess.ID),
			Project:   sess.Project,
			CreatedAt: sess.CreatedAt.UTC().Format(time.RFC3339),
			Frames:    frames,
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
	frame, ok := sessionActiveFrame(sess)
	if !ok {
		return ""
	}
	drv := state.GetDriver(frame.Command)
	if drv == nil {
		return ""
	}
	return drv.View(frame.Driver).StatusLine
}

// reconcileWindows checks whether each tracked session pane still
// exists. Missing panes are reported via EvTmuxWindowVanished.
func (r *Runtime) reconcileWindows() {
	for frameID, target := range r.sessionPanes {
		if frameID == r.activeFrameID {
			continue
		}
		alive, err := r.cfg.Tmux.PaneAlive(target)
		if err != nil {
			slog.Debug("runtime: reconcile pane failed", "frame", frameID, "pane", target, "err", err)
			continue
		}
		if !alive {
			r.Enqueue(state.EvTmuxWindowVanished{FrameID: frameID})
		}
	}
}

// findPaneOwner returns the FrameID currently active in pane 0.0.
func (r *Runtime) findPaneOwner(_ string) state.FrameID {
	return r.activeFrameID
}
