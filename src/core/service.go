package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/tmux"
)

type Service struct {
	Manager        *session.Manager
	Monitor        *tmux.Monitor
	Panes          tmux.PaneOperator
	SessionName    string
	activeWindowID string
	lastCount      int
}

func NewService(mgr *session.Manager, mon *tmux.Monitor, panes tmux.PaneOperator, sessionName string) *Service {
	return &Service{
		Manager:     mgr,
		Monitor:     mon,
		Panes:       panes,
		SessionName: sessionName,
	}
}

func (s *Service) Preview(sess *session.Session) error {
	cmds := s.buildSwapChain(sess)
	if err := s.Panes.RunChain(cmds...); err != nil {
		return err
	}
	s.activeWindowID = sess.WindowID
	return nil
}

func (s *Service) Switch(sess *session.Session) error {
	cmds := s.buildSwapChain(sess)
	if err := s.Panes.RunChain(cmds...); err != nil {
		return err
	}
	s.activeWindowID = sess.WindowID
	return s.Panes.SelectPane(s.SessionName + ":0.0")
}

func (s *Service) ActiveWindowID() string {
	return s.activeWindowID
}

func (s *Service) FocusPane(pane string) {
	s.Panes.SelectPane(s.SessionName + ":" + pane)
}

func (s *Service) LaunchTool(toolName string, args map[string]string) {
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

func (s *Service) PollStates(windowIDs []string) map[string]session.State {
	return s.Monitor.PollAll(windowIDs)
}

func (s *Service) UpdateStates(states map[string]session.State) {
	s.Manager.UpdateStates(states)
}

func (s *Service) buildSwapChain(sess *session.Session) [][]string {
	pane0 := s.SessionName + ":0.0"
	var cmds [][]string
	if s.activeWindowID != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", s.activeWindowID + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", sess.WindowID + ".0"})
	cmds = append(cmds, []string{"respawn-pane", "-k", "-t", s.SessionName + ":0.1", session.TailCommand(s.Manager.DataDir(), sess.ID)})
	return cmds
}
