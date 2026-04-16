package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type mouseLeaveMsg struct{ seq int }

const mouseLeaveTimeout = 200 * time.Millisecond

const edgeMargin = 3

func (m Model) handleMouseLeave(msg mouseLeaveMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.mouseSeq || !m.isMouseAtEdge() {
		return m, nil
	}
	m.hovering = false
	if m.anchored == "" || m.anchored == m.active {
		return m, nil
	}
	idx := m.findSessionCursorByID(m.anchored)
	if idx < 0 {
		m.anchored = ""
		return m, nil
	}
	m.cursor = idx
	return m, m.anchoredPreviewCmd()
}

func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	m.hovering = true
	m.mouseSeq++
	seq := m.mouseSeq
	leaveTimer := tea.Tick(mouseLeaveTimeout, func(time.Time) tea.Msg {
		return mouseLeaveMsg{seq: seq}
	})
	mouse := msg.Mouse()
	m.lastMouseX = mouse.X
	m.lastMouseY = mouse.Y
	idx := m.rowToItemIndex(mouse.Y)
	if idx < 0 || idx == m.cursor || idx < m.offset {
		return m, leaveTimer
	}
	m.cursor = idx
	if cmd := m.cursorPreviewCmd(); cmd != nil {
		return m, tea.Batch(cmd, leaveTimer)
	}
	return m, leaveTimer
}

func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	if mouse.Y == 0 {
		if m.active != "" {
			return m, m.deactivateCmd()
		}
		return m, m.focusCmd(mainPane)
	}
	if name, isAll, hit := m.hitTestWorkspaceChip(mouse.X, mouse.Y); hit {
		if isAll {
			m.selectedWorkspace = ""
		} else {
			m.selectedWorkspace = name
		}
		m.rebuildItems()
		return m, nil
	}
	if state, isAll, hit := m.hitTestFilterChip(mouse.X, mouse.Y); hit {
		if isAll {
			m.filter.toggleAll()
		} else {
			m.filter.toggle(state)
		}
		m.rebuildItems()
		return m, nil
	}
	if m.hitTestConnectorSummary(mouse.Y) {
		if m.active != "" {
			return m, m.deactivateCmd()
		}
		return m, m.focusCmd(mainPane)
	}
	idx := m.rowToItemIndex(mouse.Y)
	if idx < 0 {
		return m, nil
	}
	if m.items[idx].isProject {
		name := m.items[idx].project
		m.folded[name] = !m.folded[name]
		m.rebuildItems()
		return m, nil
	}
	m.cursor = idx
	if s := m.cursorSession(); s != nil {
		m.anchored = s.ID
		return m, m.focusCmd(mainPane)
	}
	return m, nil
}

const scrollStep = 3

func (m Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	bodyHeight := m.height - m.headerRowCount()
	if bodyHeight >= m.totalItemRows() {
		return m, nil
	}
	mouse := msg.Mouse()
	switch mouse.Button {
	case tea.MouseWheelUp:
		m.offset -= scrollStep
		if m.offset < 0 {
			m.offset = 0
		}
		if m.cursor < m.offset {
			m.cursor = m.offset
		}
	case tea.MouseWheelDown:
		m.offset += scrollStep
		last := len(m.items) - 1
		if m.offset > last {
			m.offset = last
		}
		if m.cursor < m.offset {
			m.cursor = m.offset
		}
	}
	return m, m.cursorPreviewCmd()
}

// hitTestConnectorSummary returns true when the given y coordinate
// falls on the connector summary row. The summary row is the last
// header row (headerRowCount()-1) and only exists when connectors
// are present.
func (m Model) hitTestConnectorSummary(y int) bool {
	if !m.hasConnectorSummary() {
		return false
	}
	return y == m.headerRowCount()-1
}

func (m Model) isMouseAtEdge() bool {
	return m.lastMouseX < edgeMargin || m.lastMouseX >= m.width-edgeMargin ||
		m.lastMouseY < edgeMargin || m.lastMouseY >= m.height-edgeMargin
}
