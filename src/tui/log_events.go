package tui

import (
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/lib/openurl"
	"github.com/takezoh/agent-roost/proto"
)

// openProject dispatches a file path or URL to the host handler. It is
// a package variable so tests can substitute a fake.
var openProject = openurl.Open

type logEventMsg struct{ event proto.ServerEvent }
type logDisconnectMsg struct{}

type backfillDoneMsg struct {
	content string
}

func (m LogModel) listenEvents() tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-m.client.Events()
		if !ok {
			return logDisconnectMsg{}
		}
		return logEventMsg{event: ev}
	}
}

func (m LogModel) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(m.height - 1)
	if m.activeTabIs("INFO") {
		m.renderInfoTab()
	}
	return m, nil
}

func (m LogModel) handleBackfillDone(msg backfillDoneMsg) (tea.Model, tea.Cmd) {
	if msg.content != "" {
		m.appendContent(msg.content)
	}
	if m.following {
		m.viewport.GotoBottom()
	}
	return m, nil
}

func (m LogModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, nil
	case "G":
		m.following = true
		m.viewport.GotoBottom()
		return m, nil
	case "g":
		m.following = false
		m.viewport.GotoTop()
		return m, nil
	case "enter":
		if cmd := m.openProjectCmd(); cmd != nil {
			return m, cmd
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.following = m.viewport.AtBottom()
	return m, cmd
}

func (m LogModel) handleLogEvent(ev proto.ServerEvent) (tea.Model, tea.Cmd) {
	switch e := ev.(type) {
	case proto.EvtSessionsChanged:
		m.currentSession = pickActiveSession(e.Sessions, e.ActiveSessionID)
		sessionChanged := m.rebuildTabs(m.currentSession)
		if e.IsPreview {
			if idx, ok := m.tabIndexByLabel("INFO"); ok {
				m.activeTab = idx
				m.following = true
			}
		}
		if m.activeTabIs("INFO") {
			m.renderInfoTab()
		}
		if sessionChanged {
			var cmds []tea.Cmd
			if m.client != nil {
				cmds = append(cmds, m.listenEvents())
			}
			cmds = append(cmds, m.backfillActiveTab())
			return m, tea.Batch(cmds...)
		}
	case proto.EvtPaneFocused:
		if e.Pane == mainPane {
			if idx := m.firstRenderedTabIndex(); idx >= 0 {
				cmd := m.switchToTabCmd(logTab(idx))
				if m.client != nil {
					return m, tea.Batch(m.listenEvents(), cmd)
				}
				return m, cmd
			}
		}
	case proto.EvtLogLine:
		tab := m.activeTabState()
		if tab != nil && tab.logPath == e.Path && e.Line != "" {
			m.appendContent(strings.TrimRight(e.Line, "\n"))
			if m.following {
				m.viewport.GotoBottom()
			}
		}
	case proto.EvtSessionFileLine:
		tab := m.activeTabState()
		if m.currentSession != nil && m.currentSession.ID == e.SessionID &&
			tab != nil && string(tab.kind) == e.Kind && e.Line != "" {
			m.appendContent(strings.TrimRight(e.Line, "\n"))
			if m.following {
				m.viewport.GotoBottom()
			}
		}
	}
	if m.client != nil {
		return m, m.listenEvents()
	}
	return m, nil
}

func pickActiveSession(sessions []proto.SessionInfo, activeID string) *proto.SessionInfo {
	if activeID == "" {
		return nil
	}
	for i := range sessions {
		if sessions[i].ID == activeID {
			s := sessions[i]
			return &s
		}
	}
	return nil
}

func (m LogModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	if mouse.Y == 0 {
		return m, m.switchToTabCmd(m.tabIndexAtX(mouse.X))
	}
	if m.activeTabIs("INFO") && m.projectLine >= 0 {
		bodyRow := mouse.Y - 1 + m.viewport.YOffset()
		if bodyRow == m.projectLine {
			if cmd := m.openProjectCmd(); cmd != nil {
				return m, cmd
			}
		}
	}
	return m, nil
}

// openProjectCmd returns a tea.Cmd that opens the current session's
// project path via the host handler. It returns nil when there is no
// project to open (INFO tab not active, no current session, or empty
// project path).
func (m LogModel) openProjectCmd() tea.Cmd {
	if !m.activeTabIs("INFO") || m.currentSession == nil {
		return nil
	}
	target := m.currentSession.Project
	if target == "" {
		return nil
	}
	return func() tea.Msg {
		if err := openProject(target); err != nil {
			slog.Warn("openurl failed", "target", target, "err", err)
		}
		return nil
	}
}
