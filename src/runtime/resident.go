package runtime

import (
	"log/slog"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

func (r *Runtime) activateSession(sessID state.SessionID, reason string) {
	sess, ok := r.state.Sessions[sessID]
	if !ok {
		return
	}
	frame, ok := sessionActiveFrame(sess)
	if !ok {
		return
	}
	paneID := r.sessionPanes[frame.ID]
	if paneID == "" {
		slog.Warn("runtime: activate session — no pane target", "session", sessID)
		return
	}
	if r.activeFrameID == frame.ID {
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
	sess, ok := r.state.Sessions[sessID]
	if !ok {
		return false
	}
	frame, ok := sessionActiveFrame(sess)
	if !ok {
		return false
	}
	paneID := r.sessionPanes[frame.ID]
	if paneID == "" {
		slog.Warn("runtime: swap-pane session skipped; pane missing", "session", sessID)
		return false
	}
	if _, ok := r.ensureMainPaneID(); !ok {
		slog.Warn("runtime: swap-pane session skipped; main pane unknown", "session", sessID)
		return false
	}
	if err := r.cfg.Tmux.SwapPane(paneID, r.mainPaneTarget()); err != nil {
		if isMissingPaneErr(err) {
			r.Enqueue(state.EvTmuxWindowVanished{FrameID: frame.ID})
		}
		slog.Warn("runtime: swap-pane session failed", "session", sessID, "pane", paneID, "err", err)
		return false
	}
	r.activeSession = sessID
	r.activeFrameID = frame.ID
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
	r.activeFrameID = ""
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
	_ = r.cfg.Tmux.SetEnv("ROOST_FRAME__main", paneID)
	return paneID, true
}

func (r *Runtime) mainPaneTarget() string {
	return r.cfg.SessionName + ":0.1"
}

func isMissingPaneErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "can't find pane")
}

func sessionPaneEnvKey(frameID state.FrameID) string {
	return "ROOST_FRAME_" + string(frameID)
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

func sessionActiveFrame(sess state.Session) (state.SessionFrame, bool) {
	if len(sess.Frames) == 0 {
		if sess.Command == "" || sess.Driver == nil {
			return state.SessionFrame{}, false
		}
		return state.SessionFrame{
			ID:            state.FrameID(sess.ID),
			Project:       sess.Project,
			Command:       sess.Command,
			LaunchOptions: sess.LaunchOptions,
			CreatedAt:     sess.CreatedAt,
			Driver:        sess.Driver,
		}, true
	}
	if sess.ActiveFrameID != "" {
		for _, f := range sess.Frames {
			if f.ID == sess.ActiveFrameID {
				return f, true
			}
		}
	}
	return sess.Frames[len(sess.Frames)-1], true
}

func sessionRootFrame(sess state.Session) (state.SessionFrame, bool) {
	if len(sess.Frames) == 0 {
		return sessionActiveFrame(sess)
	}
	return sess.Frames[0], true
}
