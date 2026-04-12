package runtime

import (
	"log/slog"

	"github.com/takezoh/agent-roost/state"
)

const activeSessionEnvKey = "ROOST_ACTIVE_SESSION"

func (r *Runtime) activateSession(sessID state.SessionID, reason string) {
	target, ok := r.windowMap[sessID]
	if !ok {
		slog.Warn("runtime: activate session — no window target", "session", sessID)
		return
	}
	if r.activeSession == sessID && target == "0" {
		return
	}

	pane0 := r.cfg.SessionName + ":0.0"
	r.logPaneSnapshot(reason, "before-main", pane0)
	if target != "0" {
		r.logPaneSnapshot(reason, "before-target", r.cfg.SessionName+":"+target+".0")
	}

	if r.activeSession != "" && r.activeSession != sessID {
		r.parkSessionFromMain(r.activeSession)
	}
	if r.activeSession == "" {
		r.parkMainFromMain()
	}
	if !r.joinSessionIntoMain(sessID) {
		return
	}
	r.logPaneSnapshot(reason, "after-main", pane0)
}

func (r *Runtime) deactivateSession() {
	if r.activeSession == "" {
		return
	}
	if !r.parkSessionFromMain(r.activeSession) {
		return
	}
	r.joinMainIntoMain()
}

func (r *Runtime) parkSessionFromMain(sessID state.SessionID) bool {
	target := r.windowMap[sessID]
	newTarget, ok := r.breakMainPaneToWindow(target, windowNameForSession(r.state.Sessions, sessID))
	if !ok {
		return false
	}
	r.windowMap[sessID] = newTarget
	r.activeSession = ""
	_ = r.cfg.Tmux.SetEnv(windowEnvKey(newTarget), string(sessID))
	_ = r.cfg.Tmux.UnsetEnv(activeSessionEnvKey)
	return true
}

func (r *Runtime) parkMainFromMain() bool {
	target, ok := r.breakMainPaneToWindow(r.mainWindow, "main")
	if !ok {
		return false
	}
	r.mainWindow = target
	_ = r.cfg.Tmux.SetEnv(mainWindowEnvKey(), target)
	return true
}

func (r *Runtime) breakMainPaneToWindow(currentTarget, name string) (string, bool) {
	pane0 := r.cfg.SessionName + ":0.0"
	if currentTarget != "" && currentTarget != "0" {
		if err := r.cfg.Tmux.BreakPane(pane0, currentTarget); err != nil {
			slog.Warn("runtime: break-pane failed", "target", currentTarget, "err", err)
			return "", false
		}
		return currentTarget, true
	}
	target, err := r.cfg.Tmux.BreakPaneToNewWindow(pane0, name)
	if err != nil {
		slog.Warn("runtime: break-pane new-window failed", "name", name, "err", err)
		return "", false
	}
	return target, true
}

func (r *Runtime) joinSessionIntoMain(sessID state.SessionID) bool {
	target := r.windowMap[sessID]
	if target == "" || target == "0" {
		slog.Warn("runtime: join session — invalid source window", "session", sessID, "target", target)
		return false
	}
	src := r.cfg.SessionName + ":" + target + ".0"
	dst := r.cfg.SessionName + ":0.0"
	if err := r.cfg.Tmux.JoinPane(src, dst, true, r.cfg.MainPaneHeightPct); err != nil {
		slog.Warn("runtime: join-pane session failed", "session", sessID, "target", target, "err", err)
		return false
	}
	_ = r.cfg.Tmux.UnsetEnv(windowEnvKey(target))
	_ = r.cfg.Tmux.SetEnv(activeSessionEnvKey, string(sessID))
	r.windowMap[sessID] = "0"
	r.activeSession = sessID
	return true
}

func (r *Runtime) joinMainIntoMain() bool {
	if r.mainWindow == "" || r.mainWindow == "0" {
		slog.Warn("runtime: join main — no parked main window")
		return false
	}
	src := r.cfg.SessionName + ":" + r.mainWindow + ".0"
	dst := r.cfg.SessionName + ":0.0"
	if err := r.cfg.Tmux.JoinPane(src, dst, true, r.cfg.MainPaneHeightPct); err != nil {
		slog.Warn("runtime: join-pane main failed", "target", r.mainWindow, "err", err)
		return false
	}
	_ = r.cfg.Tmux.UnsetEnv(mainWindowEnvKey())
	r.mainWindow = "0"
	r.activeSession = ""
	return true
}

func windowNameForSession(sessions map[state.SessionID]state.Session, sessID state.SessionID) string {
	sess, ok := sessions[sessID]
	if !ok {
		return "session:" + string(sessID)
	}
	return windowName(sess.Project, string(sessID))
}

func mainWindowEnvKey() string {
	return "ROOST_W_MAIN"
}
