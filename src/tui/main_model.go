package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

type MainModel struct {
	client          *proto.Client
	sessions        []proto.SessionInfo
	selectedProject string
	width           int
	height          int
}

type mainEventMsg struct{ event proto.ServerEvent }
type mainDisconnectMsg struct{}

func NewMainModel(client *proto.Client) MainModel {
	return MainModel{
		client: client,
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
		return m.handleEvent(msg.event)

	case tea.KeyPressMsg:
		return m, nil
	}
	return m, nil
}

func (m MainModel) handleEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.sessions = e.Sessions
	case proto.EvtProjectSelected:
		m.selectedProject = e.Project
	}
	return m, m.listenEvents()
}

func (m MainModel) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, _, err := m.client.ListSessions()
		if err != nil {
			return nil
		}
		return mainEventMsg{event: proto.EvtSessionsChanged{Sessions: sessions}}
	}
}

func (m MainModel) listenEvents() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return mainDisconnectMsg{}
		}
		return mainEventMsg{event: ev}
	}
}

func (m MainModel) projectSessions() []proto.SessionInfo {
	if m.selectedProject == "" {
		return nil
	}
	var result []proto.SessionInfo
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

func stateSymbol(s state.Status) string {
	return stateStyle(s).Render(s.Symbol())
}
