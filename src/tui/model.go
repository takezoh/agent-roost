package tui

import (
	"path/filepath"
	"sort"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/cdeck/config"
	"github.com/take/cdeck/session"
	"github.com/take/cdeck/tmux"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *session.Session
}

type Model struct {
	manager *session.Manager
	monitor *tmux.Monitor
	tmux    *tmux.Client
	cfg     *config.Config
	keys    KeyMap

	items    []listItem
	cursor   int
	folded   map[string]bool
	projects map[string]string // name -> path (セッションがなくても表示するプロジェクト)
	dialog   Dialog
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

	case sessionCreatedMsg:
		m.err = msg.err
		m.rebuildItems()
		if msg.err == nil && msg.sess != nil {
			return m, switchCmd(m.tmux, msg.sess, m.active)
		}
		return m, nil

	case statesMsg:
		m.manager.UpdateStates(map[string]session.State(msg))
		m.rebuildItems()
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		if m.dialog.Active() {
			return m.handleDialog(msg)
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	mode := m.dialog.mode
	done, result := m.dialog.Update(msg)
	if !done {
		return m, nil
	}
	if result == "" {
		return m, nil
	}
	switch mode {
	case DialogConfirmStop:
		return m, stopCmd(m.manager, result)
	case DialogProjectSelect:
		if m.dialog.addProjectOnly {
			name := filepath.Base(result)
			m.projects[name] = result
			m.rebuildItems()
			return m, nil
		}
		cmd := m.dialog.command
		if cmd == "" {
			m.dialog = NewCommandDialog(m.cfg.Session.Commands, result)
			return m, nil
		}
		return m, createCmd(m.manager, result, cmd)
	case DialogCommandSelect:
		return m, createCmd(m.manager, m.dialog.project, result)
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
		proj := m.cursorProjectPath()
		if proj != "" {
			return m, createCmd(m.manager, proj, m.cfg.Session.DefaultCommand)
		}
		m.dialog = NewProjectDialog(m.cfg.ListProjects(), m.cfg.Session.DefaultCommand)
	case key.Matches(msg, m.keys.NewCmd):
		proj := m.cursorProjectPath()
		if proj != "" {
			m.dialog = NewCommandDialog(m.cfg.Session.Commands, proj)
		} else {
			m.dialog = NewProjectDialog(m.cfg.ListProjects(), "")
		}
	case key.Matches(msg, m.keys.AddProject):
		m.dialog = NewProjectDialog(m.cfg.ListProjects(), "")
		m.dialog.addProjectOnly = true
	case key.Matches(msg, m.keys.Stop):
		if s := m.cursorSession(); s != nil {
			m.dialog = NewConfirmDialog(s.ID)
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

func (m *Model) rebuildItems() {
	byProject := m.manager.ByProject()

	// セッションがあるプロジェクト + 明示的に追加されたプロジェクトをマージ
	allProjects := make(map[string]string) // name -> path
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

type sessionCreatedMsg struct {
	sess *session.Session
	err  error
}

func createCmd(mgr *session.Manager, project, command string) tea.Cmd {
	return func() tea.Msg {
		s, err := mgr.Create(project, command)
		return sessionCreatedMsg{sess: s, err: err}
	}
}

func stopCmd(mgr *session.Manager, sessionID string) tea.Cmd {
	return func() tea.Msg {
		mgr.Stop(sessionID)
		return tickMsg{}
	}
}
