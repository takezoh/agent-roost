package tui

import (
	"encoding/json"
	"os"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

const (
	// initialBackfillLines bounds how much past content is shown when a
	// session becomes active (or a tab is first opened).
	initialBackfillLines = 2000
	// tailReadChunk is the chunk size used when scanning backwards from
	// EOF looking for the Nth-from-last newline.
	tailReadChunk = 64 * 1024
	maxLogLines   = 5000
)

type logTab int

// tabKindLog and tabKindInfo are LogModel-internal kinds used by tabs the
// TUI manages itself (the always-on LOG tab and the synthesized INFO
// tab). Driver-provided tabs carry one of state.TabKind* values.
const (
	tabKindLog  state.TabKind = "_log"
	tabKindInfo state.TabKind = "_info"
)

type tabState struct {
	label       string
	logPath     string
	kind        state.TabKind
	rendererCfg json.RawMessage
	file        *os.File
	offset      int64
	buf         string
}

type LogModel struct {
	viewport   viewport.Model
	activeTab  logTab
	appLogPath string
	tabs       []*tabState

	following      bool
	width          int
	height         int
	client         *proto.Client
	renderer       state.TabRenderer
	currentSession *proto.SessionInfo
}

func NewLogModel(appLogPath string, client *proto.Client) LogModel {
	return LogModel{
		appLogPath: appLogPath,
		tabs: []*tabState{
			{label: "LOG", logPath: appLogPath, kind: tabKindLog},
		},
		client:    client,
		activeTab: 0,
		following: true,
	}
}

func (m LogModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.client != nil {
		cmds = append(cmds, m.listenEvents())
	}
	// Initial backfill: read the tail of the app log file so the LOG
	// tab is not blank at startup. Subsequent content arrives via
	// push events from the daemon's FileRelay.
	cmds = append(cmds, m.backfillActiveTab())
	return tea.Batch(cmds...)
}

func (m LogModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case logEventMsg:
		return m.handleLogEvent(msg.event)
	case backfillDoneMsg:
		return m.handleBackfillDone(msg)
	case logDisconnectMsg:
		return m, tea.Quit
	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.following = m.viewport.AtBottom()
		return m, cmd
	}
	return m, nil
}
