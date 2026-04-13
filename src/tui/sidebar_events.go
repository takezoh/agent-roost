package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
)

type serverEventMsg struct{ event proto.ServerEvent }
type disconnectMsg struct{}

type previewDoneMsg struct {
	sessionID string
	err       error
}

type switchDoneMsg struct {
	sessionID string
	err       error
}

type deactivateDoneMsg struct {
	err error
}

func (m Model) handleServerEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.sessions = e.Sessions
		m.connectors = e.Connectors
		m.rebuildItems()
		if e.ActiveSessionID == "" {
			m.active = ""
		}
		if m.active != "" && m.findSessionCursorByID(m.active) < 0 {
			m.active = ""
		}
		if m.anchored != "" && m.findSessionCursorByID(m.anchored) < 0 {
			m.anchored = ""
		}
		if e.ActiveSessionID != "" && e.ActiveSessionID != m.active {
			m.active = e.ActiveSessionID
			m.anchored = e.ActiveSessionID
			if sc := m.findSessionCursorByID(e.ActiveSessionID); sc >= 0 {
				m.cursor = sc
			}
			return m, tea.Batch(m.listenEvents(), m.focusCmd(mainPane))
		}
		if e.ActiveSessionID == "" && m.active == "" {
			m.cursor = m.firstSessionIndex()
		}
	}
	return m, m.listenEvents()
}
