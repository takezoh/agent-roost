package tui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/agent-roost/tui/glyphs"
)

type MainModel struct {
	client          *proto.Client
	viewport        viewport.Model
	spinner         spinner.Model
	sessions        []proto.SessionInfo
	connectors      []proto.ConnectorInfo
	activeOccupant  string
	selectedProject string
	width           int
	height          int
}

type mainEventMsg struct{ event proto.ServerEvent }
type mainDisconnectMsg struct{}

func NewMainModel(client *proto.Client) MainModel {
	return MainModel{
		client:  client,
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
}

func (m MainModel) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.requestSessions(), m.listenEvents(), m.spinner.Tick)
}

func (m MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(m.height - 1) // reserve title row
		m.viewport.SetContent(m.renderContent())
		return m, nil

	case mainDisconnectMsg:
		return m, tea.Quit

	case mainEventMsg:
		return m.handleEvent(msg.event)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		animFrame++
		return m, cmd

	case tea.KeyPressMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m MainModel) handleEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.sessions = e.Sessions
		m.connectors = e.Connectors
		m.activeOccupant = e.ActiveOccupant
	case proto.EvtProjectSelected:
		m.selectedProject = e.Project
	}
	m.viewport.SetContent(m.renderContent())
	return m, m.listenEvents()
}

func (m MainModel) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, _, occupant, connectors, _, err := m.client.ListSessions()
		if err != nil {
			return nil
		}
		return mainEventMsg{event: proto.EvtSessionsChanged{Sessions: sessions, ActiveOccupant: occupant, Connectors: connectors}}
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
	return stateStyle(s).Render(glyphs.Get(s.SymbolKey()))
}
