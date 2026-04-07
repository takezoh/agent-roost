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
	idx := m.findSessionCursorByWindowID(m.anchored)
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
	if idx < 0 || idx == m.cursor {
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
		m.anchored = s.WindowID
		return m, m.focusCmd("0.0")
	}
	return m, nil
}

func (m Model) isMouseAtEdge() bool {
	return m.lastMouseX < edgeMargin || m.lastMouseX >= m.width-edgeMargin ||
		m.lastMouseY < edgeMargin || m.lastMouseY >= m.height-edgeMargin
}
