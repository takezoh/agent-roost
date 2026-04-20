package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
)

// HeaderModel is the 1-row frame tab bar rendered above the main pane.
// It subscribes to EvtSessionsChanged, finds the active session, and
// shows its frames as clickable tabs.
type HeaderModel struct {
	client   *proto.Client
	sessions []proto.SessionInfo
	width    int
}

type headerEventMsg struct{ event proto.ServerEvent }
type headerDisconnectMsg struct{}

// frameTabHitbox records the half-open x range [x0, x1) of one tab chip,
// plus the session and frame IDs needed to dispatch ActivateFrame.
type frameTabHitbox struct {
	sessionID string
	frameID   string
	x0        int
	x1        int
}

// NewHeaderModel creates a HeaderModel. client may be nil (offline/test).
func NewHeaderModel(client *proto.Client) HeaderModel {
	return HeaderModel{client: client}
}

func (m HeaderModel) Init() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return tea.Batch(m.requestSessions(), m.listenEvents())
}

func (m HeaderModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case headerDisconnectMsg:
		return m, tea.Quit

	case headerEventMsg:
		return m.handleEvent(msg.event)

	case tea.MouseClickMsg:
		if m.client != nil {
			if hit, ok := m.hitTestTab(msg.X, msg.Y); ok {
				return m, m.activateFrameCmd(hit.sessionID, hit.frameID)
			}
		}
	}
	return m, nil
}

func (m HeaderModel) handleEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	if e, ok := ev.(proto.EvtSessionsChanged); ok {
		m.sessions = e.Sessions
	}
	return m, m.listenEvents()
}

func (m HeaderModel) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, _, connectors, _, err := m.client.ListSessions()
		if err != nil {
			return nil
		}
		return headerEventMsg{event: proto.EvtSessionsChanged{Sessions: sessions, Connectors: connectors}}
	}
}

func (m HeaderModel) listenEvents() tea.Cmd {
	if m.client == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return headerDisconnectMsg{}
		}
		return headerEventMsg{event: ev}
	}
}

func (m HeaderModel) hitTestTab(x, y int) (frameTabHitbox, bool) {
	if y != 0 {
		return frameTabHitbox{}, false
	}
	var active *proto.SessionInfo
	for i := range m.sessions {
		if m.sessions[i].IsActive {
			active = &m.sessions[i]
			break
		}
	}
	if active == nil {
		return frameTabHitbox{}, false
	}
	_, boxes := frameTabLayout(*active)
	for _, h := range boxes {
		if x >= h.x0 && x < h.x1 {
			return h, true
		}
	}
	return frameTabHitbox{}, false
}

func (m HeaderModel) activateFrameCmd(sessionID, frameID string) tea.Cmd {
	return func() tea.Msg {
		_ = m.client.ActivateFrame(sessionID, frameID)
		return nil
	}
}
