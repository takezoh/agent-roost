package core

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

type Service struct {
	Manager        *session.Manager
	AgentStore     *driver.AgentStore
	Drivers        *driver.Registry
	Monitor        *tmux.Monitor
	Panes          tmux.PaneOperator
	Tracker        SessionTracker
	SessionName    string
	eventLogDir    string
	activeWindowID string
	syncActive     func(string)
	syncStatus     func(string)
	onPreview      []func(string)
	lastCount      int
}

func NewService(mgr *session.Manager, store *driver.AgentStore, drivers *driver.Registry, mon *tmux.Monitor, panes tmux.PaneOperator, sessionName, eventLogDir, activeWindowID string) *Service {
	if eventLogDir != "" {
		os.MkdirAll(eventLogDir, 0o755)
	}
	return &Service{
		Manager:        mgr,
		AgentStore:     store,
		Drivers:        drivers,
		Monitor:        mon,
		Panes:          panes,
		Tracker:        noopTracker{},
		SessionName:    sessionName,
		eventLogDir:    eventLogDir,
		activeWindowID: activeWindowID,
	}
}

// SetTracker installs a SessionTracker implementation. main wires this
// during startup with a Claude-aware tracker.
func (s *Service) SetTracker(t SessionTracker) {
	if t == nil {
		t = noopTracker{}
	}
	s.Tracker = t
}

func (s *Service) SetSyncStatus(fn func(string)) {
	s.syncStatus = fn
}

func (s *Service) SetSyncActive(fn func(string)) {
	s.syncActive = fn
}

func (s *Service) setActiveWindowID(wid string) {
	s.activeWindowID = wid
	if s.syncActive != nil {
		s.syncActive(wid)
	}
}

func (s *Service) OnPreview(fn func(sessionID string)) {
	s.onPreview = append(s.onPreview, fn)
}

func (s *Service) emitPreview(sessionID string) {
	for _, fn := range s.onPreview {
		fn(sessionID)
	}
}

func (s *Service) Preview(sess *session.Session) error {
	slog.Info("preview", "window", sess.WindowID)
	cmds := s.buildSwapChain(sess)
	if err := s.Panes.RunChain(cmds...); err != nil {
		slog.Error("preview failed", "target", sess.WindowID, "active", s.activeWindowID, "err", err)
		return err
	}
	s.setActiveWindowID(sess.WindowID)
	s.emitPreview(sess.ID)
	return nil
}

func (s *Service) Switch(sess *session.Session) error {
	slog.Info("switch", "window", sess.WindowID)
	cmds := s.buildSwapChain(sess)
	if err := s.Panes.RunChain(cmds...); err != nil {
		slog.Error("switch failed", "target", sess.WindowID, "active", s.activeWindowID, "err", err)
		return err
	}
	s.setActiveWindowID(sess.WindowID)
	return s.Panes.SelectPane(s.SessionName + ":0.0")
}

func (s *Service) Deactivate() error {
	if s.activeWindowID == "" {
		return nil
	}
	pane0 := s.SessionName + ":0.0"
	cmd := []string{"swap-pane", "-d", "-s", pane0, "-t", s.activeWindowID + ".0"}
	if err := s.Panes.RunChain(cmd); err != nil {
		return err
	}
	s.setActiveWindowID("")
	return nil
}

func (s *Service) ActiveWindowID() string {
	return s.activeWindowID
}

func (s *Service) ClearActive(windowID string) {
	if s.activeWindowID == windowID {
		slog.Info("clear active", "window", windowID)
		s.setActiveWindowID("")
	}
}

func (s *Service) ActiveSessionLogPath() string {
	if s.activeWindowID == "" {
		return ""
	}
	for _, sess := range s.Manager.All() {
		if sess.WindowID == s.activeWindowID {
			return session.LogPath(s.Manager.DataDir(), sess.ID)
		}
	}
	return ""
}

func (s *Service) FocusPane(pane string) {
	s.Panes.SelectPane(s.SessionName + ":" + pane)
}

func (s *Service) LaunchTool(toolName string, args map[string]string) {
	slog.Info("launch tool", "tool", toolName)
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}

	paletteArgs := []string{"--tui", "palette", "--tool=" + toolName}
	for k, v := range args {
		if v != "" {
			paletteArgs = append(paletteArgs, "--arg="+k+"="+v)
		}
	}

	popupCmd := resolved + " " + strings.Join(paletteArgs, " ")
	exec.Command("tmux", "display-popup", "-E", "-w", "60%", "-h", "50%", popupCmd).Start()
}

// ReapDeadSessions removes sessions whose tmux pane has died. Two paths
// converge into the ReconcileWindows cleanup loop:
//
//  1. Active session: the agent pane lives in pane SESSION:0.0 (swap-pane'd
//     in by Preview/Switch). Window 0 has remain-on-exit on so a dead pane
//     lingers as `[exited]` instead of vanishing. We poll pane_dead on 0.0
//     and, if dead with an active session, swap-pane back so the dead pane
//     returns to its session window, then kill that window.
//  2. Background sessions: the session window itself disappears (single-pane
//     window with remain-on-exit off).
//
// In both cases, the session window ends up gone and ReconcileWindows
// finalizes the in-memory cache cleanup. Returns the reaped session IDs so
// the caller can decide whether to broadcast a sessions-changed event.
func (s *Service) ReapDeadSessions() []string {
	if s.activeWindowID != "" && s.isPane00Dead() {
		s.handleActiveDeadPane()
	}

	removed, err := s.Manager.ReconcileWindows()
	if err != nil {
		slog.Warn("reconcile windows failed", "err", err)
		return nil
	}
	if len(removed) == 0 {
		return nil
	}
	ids := make([]string, 0, len(removed))
	for _, r := range removed {
		s.ClearActive(r.WindowID)
		s.AgentStore.Unbind(r.WindowID)
		ids = append(ids, r.ID)
	}
	return ids
}

// isPane00Dead returns true if pane SESSION:0.0 has pane_dead=1.
func (s *Service) isPane00Dead() bool {
	out, err := s.Panes.DisplayMessage(s.SessionName+":0.0", "#{pane_dead}")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "1"
}

// handleActiveDeadPane handles the case where the active session's agent
// process died while swap-pane'd into pane 0.0. It restores the main TUI to
// 0.0 by swapping back (Deactivate), then kills the session window which now
// contains the dead pane. The cache cleanup is left to ReconcileWindows
// running immediately after.
func (s *Service) handleActiveDeadPane() {
	activeWID := s.activeWindowID
	sess := s.Manager.FindByWindowID(activeWID)
	if sess == nil {
		s.setActiveWindowID("")
		return
	}
	slog.Info("handle active dead pane", "session", sess.ID, "window", activeWID)
	if err := s.Deactivate(); err != nil {
		slog.Warn("handle active dead: deactivate failed", "err", err)
		return
	}
	if err := s.Manager.KillWindow(activeWID); err != nil {
		slog.Warn("handle active dead: kill window failed", "window", activeWID, "err", err)
	}
}

func (s *Service) RefreshSessions() (changed bool, latest *session.Session) {
	oldCount := len(s.Manager.All())
	s.Manager.Refresh()
	sessions := s.Manager.All()
	newCount := len(sessions)
	if newCount != oldCount {
		changed = true
		slog.Info("sessions changed", "old", oldCount, "new", newCount)
		if newCount > oldCount {
			latest = sessions[len(sessions)-1]
		}
	}
	s.lastCount = newCount
	return
}

func (s *Service) Sessions() []*session.Session {
	return s.Manager.All()
}

func (s *Service) SessionsByProject() map[string][]*session.Session {
	return s.Manager.ByProject()
}

func (s *Service) PollStates(sessions []*session.Session) map[string]session.State {
	windowCommands := make(map[string]string, len(sessions))
	for _, sess := range sessions {
		windowCommands[sess.WindowID] = sess.Command
	}
	return s.Monitor.PollAll(windowCommands)
}

func (s *Service) UpdateStates(states map[string]session.State) {
	s.Manager.UpdateStates(states)
}

// SyncActiveStatusLine pushes the active session's cached status line to tmux.
func (s *Service) SyncActiveStatusLine() {
	if s.syncStatus == nil {
		return
	}
	if s.activeWindowID == "" {
		s.syncStatus("")
		return
	}
	agent := s.AgentStore.GetByWindow(s.activeWindowID)
	if agent != nil {
		s.syncStatus(agent.StatusLine)
	} else {
		s.syncStatus("")
	}
}

// ResolveAgentState returns the final display state for a session,
// merging capture-pane state with agent hook state.
func ResolveAgentState(command string, captureState session.State, agent *driver.AgentSession) session.State {
	if driver.Kind(command) != "claude" {
		return captureState
	}
	if agent == nil || agent.State == driver.AgentStateUnset {
		return session.StateIdle
	}
	switch agent.State {
	case driver.AgentStateRunning:
		return session.StateRunning
	case driver.AgentStateWaiting:
		return session.StateWaiting
	case driver.AgentStatePending:
		return session.StatePending
	case driver.AgentStateStopped:
		return session.StateStopped
	case driver.AgentStateIdle:
		return session.StateIdle
	default:
		return session.StateIdle
	}
}

// resolveWindowID finds a window ID by pane, falling back to active session.
func (s *Service) resolveWindowID(pane string) string {
	if pane != "" {
		if wid, err := s.Panes.WindowIDFromPane(pane); err == nil {
			if s.Manager.FindByWindowID(wid) != nil {
				return wid
			}
		}
	}
	return s.activeWindowID
}

// AppendEventLog writes a timestamped line to the agent session's event log file.
func (s *Service) AppendEventLog(agentSessionID, line string) {
	if s.eventLogDir == "" || agentSessionID == "" {
		return
	}
	path := filepath.Join(s.eventLogDir, agentSessionID+".log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), line)
}

// EventLogPathByWindow returns the event log file path for the active session's agent.
func (s *Service) EventLogPathByWindow(windowID string) string {
	if s.eventLogDir == "" {
		return ""
	}
	id := s.AgentStore.IDByWindow(windowID)
	if id == "" {
		return ""
	}
	return filepath.Join(s.eventLogDir, id+".log")
}

// ActiveTranscriptPath returns the transcript file path for the active session.
func (s *Service) ActiveTranscriptPath() string {
	if s.activeWindowID == "" {
		return ""
	}
	sess := s.Manager.FindByWindowID(s.activeWindowID)
	if sess == nil {
		return ""
	}
	if s.AgentStore.GetByWindow(s.activeWindowID) == nil {
		return ""
	}
	return s.transcriptPathFor(sess)
}

func (s *Service) buildSwapChain(sess *session.Session) [][]string {
	pane0 := s.SessionName + ":0.0"
	var cmds [][]string
	if s.activeWindowID != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", s.activeWindowID + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", sess.WindowID + ".0"})
	return cmds
}
