package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/proto"
)

func (m Model) requestSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, activeID, connectors, feats, err := m.client.ListSessions()
		if err != nil {
			return nil
		}
		return serverEventMsg{event: proto.EvtSessionsChanged{
			Sessions:        sessions,
			ActiveSessionID: activeID,
			Connectors:      connectors,
			Features:        feats,
		}}
	}
}

func (m Model) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return disconnectMsg{}
		}
		return serverEventMsg{event: ev}
	}
}

func (m Model) previewCmd(sess *proto.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeID, err := m.client.PreviewSession(sess.ID)
		return previewDoneMsg{sessionID: activeID, err: err}
	}
}

func (m Model) switchCmd(sess *proto.SessionInfo) tea.Cmd {
	return func() tea.Msg {
		activeID, err := m.client.SwitchSession(sess.ID)
		return switchDoneMsg{sessionID: activeID, err: err}
	}
}

func (m Model) cursorPreviewCmd() tea.Cmd {
	if s := m.cursorSession(); s != nil && s.ID != m.active {
		return m.previewCmd(s)
	}
	return nil
}

func (m Model) anchoredPreviewCmd() tea.Cmd {
	idx := m.findSessionCursorByID(m.anchored)
	if idx < 0 {
		return nil
	}
	return m.previewCmd(m.items[idx].session)
}

func (m Model) launchToolCmd(toolName string, args map[string]string) tea.Cmd {
	return func() tea.Msg {
		_ = m.client.LaunchTool(toolName, args)
		return nil
	}
}

func (m Model) deactivateCmd() tea.Cmd {
	return func() tea.Msg {
		err := m.client.PreviewProject("")
		return deactivateDoneMsg{err: err}
	}
}

func (m Model) focusCmd(pane string) tea.Cmd {
	return func() tea.Msg {
		_ = m.client.FocusPane(pane)
		return nil
	}
}
