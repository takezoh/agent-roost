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

	if r.activeSession != "" && r.activeSession != sessID && !r.parkSessionFromMain(r.activeSession) {
		return
	}
	if r.activeSession == "" && !r.parkMainFromMain() {
		return
	}
	if !r.joinSessionIntoMain(sessID) {
		return
	}
	r.logPaneSnapshot(reason, "after-main", main)
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
	paneID := r.sessionPanes[sessID]
	if paneID == "" {
		return false
	}
	if _, err := r.cfg.Tmux.BreakPaneToNewWindow(paneID, windowNameForSession(r.state.Sessions, sessID)); err != nil {
		slog.Warn("runtime: break-pane session failed", "session", sessID, "err", err)
		return false
	}
	r.activeSession = ""
	return true
}

func (r *Runtime) parkMainFromMain() bool {
	paneID, ok := r.ensureMainPaneID()
	if !ok {
		return false
	}
	if _, err := r.cfg.Tmux.BreakPaneToNewWindow(paneID, "main"); err != nil {
		slog.Warn("runtime: break-pane main failed", "err", err)
		return false
	}
	return true
}

func (r *Runtime) joinSessionIntoMain(sessID state.SessionID) bool {
	paneID := r.sessionPanes[sessID]
	if paneID == "" {
		return false
	}
	if err := r.cfg.Tmux.JoinPane(paneID, r.mainPaneTarget(), true, r.cfg.MainPaneHeightPct); err != nil {
		slog.Warn("runtime: join-pane session failed", "session", sessID, "pane", paneID, "err", err)
		return false
	}
	r.activeSession = sessID
	return true
}

func (r *Runtime) joinMainIntoMain() bool {
	paneID, ok := r.ensureMainPaneID()
	if !ok {
		return false
	}
	if err := r.cfg.Tmux.JoinPane(paneID, r.mainPaneTarget(), true, r.cfg.MainPaneHeightPct); err != nil {
		slog.Warn("runtime: join-pane main failed", "pane", paneID, "err", err)
		return false
	}
	r.activeSession = ""
	return true
}

func (r *Runtime) ensureMainPaneID() (string, bool) {
	if r.mainPaneID != "" {
		return r.mainPaneID, true
	}
	paneID, err := r.cfg.Tmux.PaneID(r.mainPaneTarget())
	if err != nil || paneID == "" {
		slog.Warn("runtime: pane-id lookup failed", "target", r.mainPaneTarget(), "err", err)
		return "", false
	}
	r.mainPaneID = paneID
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
