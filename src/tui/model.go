package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"

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
	spinner  spinner.Model
	help     help.Model

	sessions          []proto.SessionInfo
	connectors        []proto.ConnectorInfo
	items             []listItem
	cursor            int
	offset            int // first visible item index for scrolling
	folded            map[string]bool
	filter            statusFilter
	selectedWorkspace string   // active workspace name; default = config.DefaultWorkspaceName
	workspaces        []string // sorted distinct workspace names (always includes "default")
	active            string
	anchored          string
	notifications     map[string][]notifEntry // sessionID → ring buffer (latest 3)
	mouseSeq   int
	hovering   bool
	lastMouseX int
	lastMouseY int
	width      int
	height     int
}

func NewModel(client *proto.Client, cfg *config.Config) Model {
	feats := features.FromConfig(cfg.Features.Enabled, features.All())
	return Model{
		client:            client,
		cfg:               cfg,
		registry:          tools.DefaultRegistry(feats),
		features:          feats,
		keys:              DefaultKeyMap(),
		folded:            make(map[string]bool),
		filter:            allOnFilter(),
		selectedWorkspace: config.DefaultWorkspaceName,
		cursor:            -1,
		spinner:           spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		help:              help.New(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.requestSessions(),
		m.listenEvents(),
		m.spinner.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
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

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		animFrame++
		return m, cmd

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
