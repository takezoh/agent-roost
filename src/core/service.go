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

// ReapDeadSessions removes sessions whose tmux window has disappeared
// (typically because the agent process exited and tmux auto-killed the
// single-pane window). Returns the IDs of reaped sessions so the caller can
// decide whether to broadcast a sessions-changed event.
func (s *Service) ReapDeadSessions() []string {
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

// HandleSessionStart binds an agent session to a tmux window and persists the
// binding as a tmux window user option so it survives Coordinator restart.
// Uses pane → WindowID for identification, falls back to active session.
func (s *Service) HandleSessionStart(pane, agentSessionID string) bool {
	windowID := s.resolveWindowID(pane)
	if windowID == "" {
		return false
	}
	changed := s.AgentStore.Bind(windowID, agentSessionID)
	s.Manager.SetAgentSessionID(windowID, agentSessionID)
	return changed
}

// HandleStateChange updates the agent state by agentSessionID.
func (s *Service) HandleStateChange(agentSessionID string, state driver.AgentState) bool {
	return s.AgentStore.UpdateState(agentSessionID, state)
}

// HandleStateChangeWithContext updates agent state, auto-binding if the session is unknown.
func (s *Service) HandleStateChangeWithContext(agentSessionID string, state driver.AgentState, pane string) bool {
	if s.AgentStore.Get(agentSessionID) == nil && pane != "" {
		s.HandleSessionStart(pane, agentSessionID)
	}
	return s.AgentStore.UpdateState(agentSessionID, state)
}

// HandleStatusLine updates the agent status line by agentSessionID.
// If the agent is bound to the active session, syncs to tmux.
func (s *Service) HandleStatusLine(agentSessionID, line string) bool {
	changed := s.AgentStore.UpdateStatusLine(agentSessionID, line)
	if s.syncStatus != nil && s.activeWindowID != "" {
		active := s.AgentStore.GetByWindow(s.activeWindowID)
		if active != nil && active.ID == agentSessionID {
			s.syncStatus(line)
		}
	}
	return changed
}

// HandleTranscriptPath records the JSONL transcript path Claude reports for an
// agent session. The path is cached in the AgentStore for runtime lookups and
// persisted as a tmux window user option so cold-boot restore can recover it
// before the first hook event of the new process arrives.
func (s *Service) HandleTranscriptPath(agentSessionID, transcriptPath string) bool {
	if agentSessionID == "" || transcriptPath == "" {
		return false
	}
	changed := s.AgentStore.UpdateTranscriptPath(agentSessionID, transcriptPath)
	if !changed {
		return false
	}
	if wid := s.AgentStore.WindowIDByAgent(agentSessionID); wid != "" {
		s.Manager.SetAgentTranscriptPath(wid, transcriptPath)
	}
	return true
}

// TranscriptPathByAgent returns the transcript path for a given agent session ID.
func (s *Service) TranscriptPathByAgent(agentSessionID string) string {
	agent := s.AgentStore.Get(agentSessionID)
	if agent == nil {
		return ""
	}
	return agent.TranscriptPath
}

// UpdateStatusFromTranscript reads new transcript content and updates the status line.
func (s *Service) UpdateStatusFromTranscript(agentSessionID string) bool {
	path := s.TranscriptPathByAgent(agentSessionID)
	line, changed := s.Tracker.Update(agentSessionID, path)
	if changed {
		s.HandleStatusLine(agentSessionID, line)
	}
	return changed
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

// ActiveTranscriptPath returns the transcript JSONL path for the active Claude session.
func (s *Service) ActiveTranscriptPath() string {
	if s.activeWindowID == "" {
		return ""
	}
	sess := s.Manager.FindByWindowID(s.activeWindowID)
	if sess == nil || driver.Kind(sess.Command) != "claude" {
		return ""
	}
	agent := s.AgentStore.GetByWindow(s.activeWindowID)
	if agent == nil {
		return ""
	}
	return agent.TranscriptPath
}

// ResolveAgentMeta resolves metadata from agent transcript files for windows
// that have already received a transcript path via hook events. Unbound
// windows and windows whose agent session has no transcript path yet are
// skipped — both bindings and transcript paths only become known through
// hook events.
func (s *Service) ResolveAgentMeta() bool {
	fsys := os.DirFS("/")
	changed := false
	for _, sess := range s.Manager.All() {
		agentID := s.AgentStore.IDByWindow(sess.WindowID)
		if agentID == "" {
			continue
		}
		agent := s.AgentStore.Get(agentID)
		if agent == nil || agent.TranscriptPath == "" {
			continue
		}
		meta := s.Drivers.Get(sess.Command).ResolveMeta(fsys, agent.TranscriptPath)
		if meta.Title == "" && meta.LastPrompt == "" && len(meta.Subjects) == 0 {
			continue
		}
		if s.AgentStore.UpdateMeta(agentID, meta) {
			changed = true
		}
	}
	return changed
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
