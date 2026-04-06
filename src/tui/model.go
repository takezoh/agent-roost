package tui

import (
	"log/slog"
	"sort"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *core.SessionInfo
}

type Model struct {
	client   *core.Client
	cfg      *config.Config
	registry *Registry
	keys     KeyMap

	sessions []core.SessionInfo
	items    []listItem
	cursor   int
	folded   map[string]bool
	projects map[string]string
	active   string
	width    int
	height   int
	err      error
}

type serverEventMsg core.Message
type disconnectMsg struct{}

type previewDoneMsg struct {
	windowID string
	err      error
}

type switchDoneMsg struct {
	windowID string
	err      error
}

func NewModel(client *core.Client, cfg *config.Config) Model {
	return Model{
		client:   client,
		cfg:      cfg,
		registry: DefaultRegistry(),
		keys:     DefaultKeyMap(),
		folded:   make(map[string]bool),
		projects: make(map[string]string),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.requestSessions(),
		m.listenEvents(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case disconnectMsg:
		return m, tea.Quit

	case serverEventMsg:
		return m.handleServerEvent(core.Message(msg))

	case previewDoneMsg:
		if msg.err == nil {
			m.active = msg.windowID
		} else {
			m.err = msg.err
		}
		return m, m.focusCmd("0.2")

	case switchDoneMsg:
		if msg.err == nil {
			m.active = msg.windowID
		} else {
			m.err = msg.err
		}
		return m, m.focusCmd("0.0")

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleServerEvent(msg core.Message) (tea.Model, tea.Cmd) {
	switch msg.Event {
	case "sessions-changed":
		m.sessions = msg.Sessions
		m.rebuildItems()
	case "states-updated":
		for i := range m.sessions {
			if st, ok := msg.States[m.sessions[i].WindowID]; ok {
				m.sessions[i].State = st
			}
		}
		m.rebuildItems()
	}
	return m, m.listenEvents()
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		if s := m.cursorSession(); s != nil && s.WindowID != m.active {
			return m, m.previewCmd(s)
		}
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		if s := m.cursorSession(); s != nil && s.WindowID != m.active {
			return m, m.previewCmd(s)
		}
	case key.Matches(msg, m.keys.Enter):
		if s := m.cursorSession(); s != nil {
			return m, m.switchCmd(s)
		}
	case key.Matches(msg, m.keys.New):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
			"command": m.cfg.Session.DefaultCommand,
		})
	case key.Matches(msg, m.keys.NewCmd):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
		})
	case key.Matches(msg, m.keys.AddProject):
		return m, m.launchToolCmd("add-project", nil)
	case key.Matches(msg, m.keys.Stop):
		if s := m.cursorSession(); s != nil {
			return m, m.launchToolCmd("stop-session", map[string]string{
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

// --- tea.Cmd wrappers ---

func (m Model) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.client.ListSessions()
		if err != nil {
			slog.Error("list-sessions failed", "err", err)
			return nil
		}
		msg := core.NewEvent("sessions-changed")
		msg.Sessions = sessions
		return serverEventMsg(msg)
	}
}

func (m Model) listenEvents() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.client.Events()
		if !ok {
			return disconnectMsg{}
		}
		return serverEventMsg(msg)
	}
}

func (m Model) previewCmd(sess *core.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		err := m.client.PreviewSession(sess.ID, m.active)
		return previewDoneMsg{windowID: sess.WindowID, err: err}
	}
}

func (m Model) switchCmd(sess *core.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		err := m.client.SwitchSession(sess.ID, m.active)
		return switchDoneMsg{windowID: sess.WindowID, err: err}
	}
}

func (m Model) launchToolCmd(toolName string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		m.client.LaunchTool(toolName, args)
		return nil
	}
}

func (m Model) focusCmd(pane string) tea.Cmd {
	return func() tea.Msg {
		m.client.FocusPane(pane)
		return nil
	}
}

// --- list building ---

func (m *Model) rebuildItems() {
	byProject := make(map[string][]core.SessionInfo)
	allProjects := make(map[string]string)

	for i := range m.sessions {
		s := &m.sessions[i]
		name := s.Name()
		byProject[name] = append(byProject[name], *s)
		allProjects[name] = s.Project
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
			for i := range byProject[name] {
				m.items = append(m.items, listItem{project: name, projectPath: path, session: &byProject[name][i]})
			}
		}
	}
	if m.cursor >= len(m.items) && len(m.items) > 0 {
		m.cursor = len(m.items) - 1
	}
}

// --- cursor helpers ---

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

func (m Model) cursorSession() *core.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return m.items[m.cursor].session
}
