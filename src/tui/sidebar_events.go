package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/features"
	"github.com/takezoh/agent-roost/proto"
)

type serverEventMsg struct{ event proto.ServerEvent }
type disconnectMsg struct{}

// notifEntry holds one OSC in-pane notification for a session.
type notifEntry struct {
	Cmd   int
	Title string
	Body  string
}

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
		if len(e.Features) > 0 {
			m.features = features.FromConfig(stringSliceToMap(e.Features), features.All())
		}
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
			m.cursor = -1
			m.anchored = ""
		}
	case proto.EvtAgentNotification:
		if m.notifications == nil {
			m.notifications = make(map[string][]notifEntry)
		}
		nb := append(m.notifications[e.SessionID], notifEntry{Cmd: e.Cmd, Title: e.Title, Body: e.Body})
		const maxNotif = 3
		if len(nb) > maxNotif {
			nb = nb[len(nb)-maxNotif:]
		}
		m.notifications[e.SessionID] = nb
	}
	return m, m.listenEvents()
}

// latestNotifLine returns a single display string for the most recent
// notification for the given session, or "" if none.
func (m Model) latestNotifLine(sessionID string) string {
	nb := m.notifications[sessionID]
	if len(nb) == 0 {
		return ""
	}
	n := nb[len(nb)-1]
	if n.Title != "" && n.Body != "" {
		return n.Title + ": " + n.Body
	}
	if n.Title != "" {
		return n.Title
	}
	return n.Body
}

// stringSliceToMap converts a list of enabled flag names into the map form
// that [features.FromConfig] expects (all values true).
func stringSliceToMap(names []string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}
