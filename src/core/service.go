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
	"github.com/take/agent-roost/state"
	"github.com/take/agent-roost/tmux"
)

type Service struct {
	Manager        *session.Manager
	AgentStore     *driver.AgentStore
	Drivers        *driver.Registry
	States         state.Store
	Observers      *driver.ObserverRegistry
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

func NewService(mgr *session.Manager, store *driver.AgentStore, drivers *driver.Registry, states state.Store, observers *driver.ObserverRegistry, panes tmux.PaneOperator, sessionName, eventLogDir, activeWindowID string) *Service {
	if eventLogDir != "" {
		os.MkdirAll(eventLogDir, 0o755)
	}
	return &Service{
		Manager:        mgr,
		AgentStore:     store,
		Drivers:        drivers,
		States:         states,
		Observers:      observers,
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

// CreateSession spawns a new session and registers a state observer for it.
// All session creation must go through this method (never Manager.Create
// directly) so the Observer registry stays in lockstep with the Manager
// cache and state.Store.
func (s *Service) CreateSession(project, command string) (*session.Session, error) {
	sess, err := s.Manager.Create(project, command)
	if err != nil {
		return nil, err
	}
	s.Observers.Spawn(sess.WindowID, sess.Command)
	return sess, nil
}

// StopSession kills a session window, removes its observer, and clears its
// state.Store entry. All session stops must go through this method.
func (s *Service) StopSession(id string) error {
	sess := s.Manager.FindByID(id)
	if sess == nil {
		return nil
	}
	windowID := sess.WindowID
	if err := s.Manager.Stop(id); err != nil {
		return err
	}
	s.Observers.Remove(windowID)
	s.ClearActive(windowID)
	return nil
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

// ResolveWindowID finds a window ID by pane, falling back to active session.
// Exported so the Server hook handler can route events to per-window
// driver observers.
func (s *Service) ResolveWindowID(pane string) string {
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
