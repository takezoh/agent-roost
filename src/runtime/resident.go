package runtime

import (
	"log/slog"

	"github.com/takezoh/agent-roost/state"
)

func (r *Runtime) activateSession(sessID state.SessionID, reason string) {
	paneID := r.sessionPanes[sessID]
	if paneID == "" {
		slog.Warn("runtime: activate session — no pane target", "session", sessID)
		return
	}
	if r.activeSession == sessID {
		return
	}

	main := r.mainPaneTarget()
	r.logPaneSnapshot(reason, "before-main", main)
	r.logPaneSnapshot(reason, "before-target", paneID)

	if !r.swapSessionIntoMain(sessID) {
		return
	}
	r.logPaneSnapshot(reason, "after-main", main)
}

func (r *Runtime) deactivateSession() {
	if r.activeSession == "" {
		return
	}
	r.swapMainIntoMain()
}

func (r *Runtime) swapSessionIntoMain(sessID state.SessionID) bool {
	paneID := r.sessionPanes[sessID]
	if paneID == "" {
		slog.Warn("runtime: swap-pane session skipped; pane missing", "session", sessID)
		return false
	}
	if _, ok := r.ensureMainPaneID(); !ok {
		slog.Warn("runtime: swap-pane session skipped; main pane unknown", "session", sessID)
		return false
	}
	if err := r.cfg.Tmux.SwapPane(paneID, r.mainPaneTarget()); err != nil {
		slog.Warn("runtime: swap-pane session failed", "session", sessID, "pane", paneID, "err", err)
		return false
	}
	r.activeSession = sessID
	return true
}

func (r *Runtime) swapMainIntoMain() bool {
	if r.activeSession == "" {
		return true
	}
	paneID := r.sessionPanes["_main"]
	if paneID == "" {
		return false
	}

	if err := r.cfg.Tmux.SwapPane(paneID, r.mainPaneTarget()); err != nil {
		slog.Warn("runtime: swap-pane main failed", "pane", paneID, "err", err)
		return false
	}
	r.activeSession = ""
	return true
}

func (r *Runtime) ensureMainPaneID() (string, bool) {
	if id := r.sessionPanes["_main"]; id != "" {
		return id, true
	}
	paneID, err := r.cfg.Tmux.PaneID(r.mainPaneTarget())
	if err != nil || paneID == "" {
		slog.Warn("runtime: pane-id lookup failed", "target", r.mainPaneTarget(), "err", err)
		return "", false
	}
	r.sessionPanes["_main"] = paneID
	_ = r.cfg.Tmux.SetEnv("ROOST_SESSION__main", paneID)
	return paneID, true
}

func (r *Runtime) mainPaneTarget() string {
	return r.cfg.SessionName + ":0.0"
}

func windowNameForSession(sessions map[state.SessionID]state.Session, sessID state.SessionID) string {
	sess, ok := sessions[sessID]
	if !ok {
		return "session:" + string(sessID)
	}
	return windowName(sess.Project, string(sessID))
}

func sessionPaneEnvKey(sessID state.SessionID) string {
	return "ROOST_SESSION_" + string(sessID)
}

type paneSize struct {
	width  int
	height int
}

func (r *Runtime) mainPaneSize() paneSize {
	width, height, err := r.cfg.Tmux.PaneSize(r.mainPaneTarget())
	if err != nil {
		slog.Debug("runtime: pane-size lookup failed", "target", r.mainPaneTarget(), "err", err)
		return paneSize{}
	}
	return paneSize{width: width, height: height}
}

func (r *Runtime) resizeWindowToMain(target string, size paneSize) {
	if size.width == 0 || size.height == 0 {
		return
	}
	if err := r.cfg.Tmux.ResizeWindow(target, size.width, size.height); err != nil {
		slog.Debug("runtime: resize-window failed", "target", target, "width", size.width, "height", size.height, "err", err)
	}
}
