package tui

import (
	"log/slog"

	tea "charm.land/bubbletea/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

type MainModel struct {
	client          *core.Client
	drivers         *driver.Registry
	sessions        []core.SessionInfo
	selectedProject string
	width           int
	height          int
}

type mainEventMsg core.Message
type mainDisconnectMsg struct{}

func NewMainModel(client *core.Client) MainModel {
	return MainModel{
		client: client,
		drivers: driver.DefaultRegistry(),
	}
}

func (m MainModel) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.requestSessions(), m.listenEvents())
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case mainDisconnectMsg:
		return m, tea.Quit

	case mainEventMsg:
		return m.handleEvent(core.Message(msg))

	case tea.KeyPressMsg:
		return m, nil
	}
	return m, nil
}

func (m MainModel) handleEvent(msg core.Message) (tea.Model, tea.Cmd) {
	switch msg.Event {
	case "sessions-changed":
		m.sessions = msg.Sessions
	case "states-updated":
		for i := range m.sessions {
			if st, ok := msg.States[m.sessions[i].WindowID]; ok {
				m.sessions[i].State = st
			}
		}
	case "project-selected":
		m.selectedProject = msg.SelectedProject
	}
	return m, m.listenEvents()
}

func (m MainModel) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, _, err := m.client.ListSessions()
		if err != nil {
			slog.Error("list-sessions failed", "err", err)
			return nil
		}
		msg := core.NewEvent("sessions-changed")
		msg.Sessions = sessions
		return mainEventMsg(msg)
	}
}

func (m MainModel) listenEvents() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-m.client.Events()
		if !ok {
			return mainDisconnectMsg{}
		}
		return mainEventMsg(msg)
	}
}

func (m MainModel) projectSessions() []core.SessionInfo {
	if m.selectedProject == "" {
		return nil
	}
	var result []core.SessionInfo
	for _, s := range m.sessions {
		if s.Project == m.selectedProject {
			result = append(result, s)
		}
	}
	return result
}

func (m MainModel) selectedProjectName() string {
	for _, s := range m.sessions {
		if s.Project == m.selectedProject {
			return s.Name()
		}
	}
	return ""
}

func stateSymbol(s session.State) string {
	return stateStyle(s).Render(s.Symbol())
}
