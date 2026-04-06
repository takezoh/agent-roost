package tui

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/tmux"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *session.Session
}

type Model struct {
	manager  *session.Manager
	monitor  *tmux.Monitor
	tmux     *tmux.Client
	cfg      *config.Config
	registry *Registry
	keys     KeyMap

	items    []listItem
	cursor   int
	folded   map[string]bool
	projects map[string]string
	active   string
	width    int
	height   int
	err      error
}

type tickMsg struct{}
type statesMsg map[string]session.State

func NewModel(mgr *session.Manager, mon *tmux.Monitor, tc *tmux.Client, cfg *config.Config) Model {
	m := Model{
		manager:  mgr,
		monitor:  mon,
		tmux:     tc,
		cfg:      cfg,
		registry: DefaultRegistry(),
		keys:     DefaultKeyMap(),
		folded:   make(map[string]bool),
		projects: make(map[string]string),
	}
	m.rebuildItems()
	return m
}

func (m Model) Init() tea.Cmd {
	return doTick(time.Duration(m.cfg.Monitor.PollIntervalMs) * time.Millisecond)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		oldCount := len(m.manager.All())
		m.manager.Load()
		m.manager.Reconcile()
		newCount := len(m.manager.All())
		if newCount != oldCount {
			slog.Info("sessions changed", "old", oldCount, "new", newCount)
			m.rebuildItems()
			if newCount > oldCount {
				sessions := m.manager.All()
				if len(sessions) > 0 {
					return m, tea.Batch(
						switchCmd(m.tmux, sessions[len(sessions)-1], m.active),
						doTick(time.Duration(m.cfg.Monitor.PollIntervalMs)*time.Millisecond),
					)
				}
			}
		}
		ids := windowIDs(m.manager.All())
		return m, tea.Batch(
			pollCmd(m.monitor, ids),
			doTick(time.Duration(m.cfg.Monitor.PollIntervalMs)*time.Millisecond),
		)

	case sessionSwitchedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.active = msg.windowID
		m.err = nil
		return m, focusPaneCmd(m.tmux, msg.focusPane)

	case statesMsg:
		m.manager.UpdateStates(map[string]session.State(msg))
		m.rebuildItems()
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		if s := m.cursorSession(); s != nil && s.WindowID != m.active {
			return m, previewCmd(m.tmux, s, m.active)
		}
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		if s := m.cursorSession(); s != nil && s.WindowID != m.active {
			return m, previewCmd(m.tmux, s, m.active)
		}
	case key.Matches(msg, m.keys.Enter):
		if s := m.cursorSession(); s != nil {
			return m, switchCmd(m.tmux, s, m.active)
		}
	case key.Matches(msg, m.keys.New):
		return m, popupToolCmd(m.tmux, "new-session", map[string]string{
			"project": m.cursorProjectPath(),
			"command": m.cfg.Session.DefaultCommand,
		})
	case key.Matches(msg, m.keys.NewCmd):
		return m, popupToolCmd(m.tmux, "new-session", map[string]string{
			"project": m.cursorProjectPath(),
		})
	case key.Matches(msg, m.keys.AddProject):
		return m, popupToolCmd(m.tmux, "add-project", nil)
	case key.Matches(msg, m.keys.Stop):
		if s := m.cursorSession(); s != nil {
			return m, popupToolCmd(m.tmux, "stop-session", map[string]string{
				"session_id": s.ID,
			})
		}
	case key.Matches(msg, m.keys.Toggle):
		name := m.cursorProjectName()
		if name != "" {
			m.folded[name] = !m.folded[name]
			m.rebuildItems()
		}
	}
	return m, nil
}

func popupToolCmd(tc *tmux.Client, toolName string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		exe, _ := os.Executable()
		resolved, err := filepath.EvalSymlinks(exe)
		if err != nil {
			resolved = exe
		}

		paletteArgs := []string{"--palette", "--tool=" + toolName}
		for k, v := range args {
			if v != "" {
				paletteArgs = append(paletteArgs, "--arg="+k+"="+v)
			}
		}

		popupCmd := resolved + " " + strings.Join(paletteArgs, " ")
		cmd := exec.Command("tmux", "display-popup", "-E", "-w", "60%", "-h", "50%", popupCmd)
		cmd.Start()
		return nil
	}
}

// --- list building ---

func (m *Model) rebuildItems() {
	byProject := m.manager.ByProject()
	allProjects := make(map[string]string)
	for name, sessions := range byProject {
		allProjects[name] = sessions[0].Project
	}
	for name, path := range m.projects {
		if _, exists := allProjects[name]; !exists {
			allProjects[name] = path
		}
	}

	names := make([]string, 0, len(allProjects))
	for name := range allProjects {
		names = append(names, name)
	}
	sort.Strings(names)

	m.items = m.items[:0]
	for _, name := range names {
		path := allProjects[name]
		m.items = append(m.items, listItem{isProject: true, project: name, projectPath: path})
		if !m.folded[name] {
			for _, s := range byProject[name] {
				m.items = append(m.items, listItem{project: name, projectPath: path, session: s})
			}
		}
	}
	if m.cursor >= len(m.items) && len(m.items) > 0 {
		m.cursor = len(m.items) - 1
	}
}

func (m Model) cursorProjectPath() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].projectPath
}

func (m Model) cursorProjectName() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].project
}

func (m Model) cursorSession() *session.Session {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return m.items[m.cursor].session
}

// --- tea.Cmd helpers ---

func windowIDs(sessions []*session.Session) []string {
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.WindowID
	}
	return ids
}

func doTick(interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(interval)
		return tickMsg{}
	}
}

func pollCmd(mon *tmux.Monitor, windowIDs []string) tea.Cmd {
	return func() tea.Msg {
		return statesMsg(mon.PollAll(windowIDs))
	}
}

type sessionSwitchedMsg struct {
	windowID  string
	focusPane string
	err       error
}

func focusPaneCmd(tc *tmux.Client, pane string) tea.Cmd {
	return func() tea.Msg {
		tc.SelectPane(tc.SessionName + ":" + pane)
		return nil
	}
}

func buildSwapChain(sn string, sess *session.Session, activeWindowID string) [][]string {
	pane0 := sn + ":0.0"
	var cmds [][]string
	if activeWindowID != "" {
		cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", activeWindowID + ".0"})
	}
	cmds = append(cmds, []string{"swap-pane", "-d", "-s", pane0, "-t", sess.WindowID + ".0"})
	cmds = append(cmds, []string{"respawn-pane", "-k", "-t", sn + ":0.1", session.TailCommand(sess.ID)})
	return cmds
}

func previewCmd(tc *tmux.Client, sess *session.Session, activeWindowID string) tea.Cmd {
	return func() tea.Msg {
		cmds := buildSwapChain(tc.SessionName, sess, activeWindowID)
		if err := tc.RunChain(cmds...); err != nil {
			return sessionSwitchedMsg{err: err}
		}
		return sessionSwitchedMsg{windowID: sess.WindowID, focusPane: "0.2"}
	}
}

func switchCmd(tc *tmux.Client, sess *session.Session, activeWindowID string) tea.Cmd {
	return func() tea.Msg {
		cmds := buildSwapChain(tc.SessionName, sess, activeWindowID)
		if err := tc.RunChain(cmds...); err != nil {
			return sessionSwitchedMsg{err: err}
		}
		return sessionSwitchedMsg{windowID: sess.WindowID, focusPane: "0.0"}
	}
}
