package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/features"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tools"
)

type Model struct {
	client   *proto.Client
	cfg      *config.Config
	registry *tools.Registry
	keys     KeyMap
	features features.Set

	sessions   []proto.SessionInfo
	connectors []proto.ConnectorInfo
	items      []listItem
	cursor     int
	offset     int // first visible item index for scrolling
	folded     map[string]bool
	filter     statusFilter
	active     string
	anchored   string
	mouseSeq   int
	hovering   bool
	lastMouseX int
	lastMouseY int
	width      int
	height     int
}

func NewModel(client *proto.Client, cfg *config.Config) Model {
	return Model{
		client:   client,
		cfg:      cfg,
		registry: tools.DefaultRegistry(),
		keys:     DefaultKeyMap(),
		folded:   make(map[string]bool),
		filter:   allOnFilter(),
		cursor:   -1,
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
		return m.handleServerEvent(msg.event)

	case previewDoneMsg:
		if msg.err == nil && msg.sessionID != "" {
			m.active = msg.sessionID
		}
		return m, m.focusCmd(sidebarPane)

	case switchDoneMsg:
		if msg.err == nil && msg.sessionID != "" {
			m.active = msg.sessionID
			m.anchored = msg.sessionID
		}
		return m, m.focusCmd(mainPane)

	case deactivateDoneMsg:
		if msg.err == nil {
			m.active = ""
			m.anchored = ""
		}
		return m, m.focusCmd(mainPane)

	case mouseLeaveMsg:
		return m.handleMouseLeave(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg)
	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, nil
		}
		return m.handleKey(msg)
	}
	return m, nil
}
