package tui

import (
	"sort"
	"strings"

	"github.com/takezoh/agent-roost/proto"
)

type listItem struct {
	isProject   bool
	project     string
	projectPath string
	session     *proto.SessionInfo
	rows        int
}

func (li *listItem) SetRows(rendered string) {
	li.rows = strings.Count(rendered, "\n") + 1
}

func (m *Model) rebuildItems() {
	var prev listItem
	if m.cursor >= 0 && m.cursor < len(m.items) {
		prev = m.items[m.cursor]
	}

	byProject := make(map[string][]proto.SessionInfo)
	allProjects := make(map[string]string)
	for i := range m.sessions {
		s := &m.sessions[i]
		if !m.filter.matches(s.State) {
			continue
		}
		name := s.Name()
		byProject[name] = append(byProject[name], *s)
		allProjects[name] = s.Project
	}

	names := make([]string, 0, len(allProjects))
	for name := range allProjects {
		names = append(names, name)
	}
	sort.Strings(names)

	m.items = m.items[:0]
	for _, name := range names {
		path := allProjects[name]
		m.items = append(m.items, listItem{isProject: true, project: name, projectPath: path})
		if m.folded[name] {
			continue
		}
		for i := range byProject[name] {
			s := &byProject[name][i]
			m.items = append(m.items, listItem{project: name, projectPath: path, session: s})
		}
	}

	m.restoreCursor(prev)
}

func (m *Model) restoreCursor(prev listItem) {
	if prev.session != nil {
		for i, item := range m.items {
			if item.session != nil && item.session.ID == prev.session.ID {
				m.cursor = i
				return
			}
		}
		for i, item := range m.items {
			if item.project == prev.project {
				m.cursor = i
				return
			}
		}
	} else if prev.isProject {
		for i, item := range m.items {
			if item.isProject && item.project == prev.project {
				m.cursor = i
				return
			}
		}
	}
	if len(m.items) == 0 {
		m.cursor = -1
		return
	}
	if m.cursor >= len(m.items) {
		m.cursor = len(m.items) - 1
	}
}

func (m Model) totalItemRows() int {
	rows := 0
	for _, item := range m.items {
		rows += item.rows
	}
	return rows
}

func (m *Model) ensureCursorVisible(bodyHeight int) {
	if m.cursor < 0 || len(m.items) == 0 || bodyHeight <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	rows := 0
	for i := m.offset; i <= m.cursor && i < len(m.items); i++ {
		rows += m.items[i].rows
	}
	for rows > bodyHeight && m.offset < m.cursor {
		rows -= m.items[m.offset].rows
		m.offset++
	}
}

func (m Model) visibleEnd(bodyHeight int) int {
	rows := 0
	for i := m.offset; i < len(m.items); i++ {
		if rows+m.items[i].rows > bodyHeight {
			return i
		}
		rows += m.items[i].rows
	}
	return len(m.items)
}

func (m Model) rowToItemIndex(y int) int {
	row := m.headerRowCount()
	if m.offset > 0 {
		row++
	}
	sticky := stickyProject(m.items, m.offset)
	if sticky != "" {
		if y == row {
			return m.findProjectHeader(sticky)
		}
		row++
	}
	for i := m.offset; i < len(m.items); i++ {
		item := m.items[i]
		if item.rows <= 0 {
			continue
		}
		if y >= row && y < row+item.rows {
			return i
		}
		row += item.rows
	}
	return -1
}

func (m Model) findProjectHeader(name string) int {
	for i, item := range m.items {
		if item.isProject && item.project == name {
			return i
		}
	}
	return -1
}

func (m Model) cursorSession() *proto.SessionInfo {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return nil
	}
	return m.items[m.cursor].session
}

func (m Model) cursorProjectPath() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].projectPath
}

func (m Model) cursorProjectName() string {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return ""
	}
	return m.items[m.cursor].project
}

func (m Model) findSessionCursorByID(id string) int {
	for i, item := range m.items {
		if item.session != nil && item.session.ID == id {
			return i
		}
	}
	return -1
}

func (m Model) headerRowCount() int {
	n := 3
	if m.hasConnectorSummary() {
		n++
	}
	return n
}

func (m Model) hasConnectorSummary() bool {
	return m.connectorSummaryLine() != ""
}

func (m Model) connectorSummaryLine() string {
	if len(m.connectors) == 0 {
		return ""
	}
	var parts []string
	for _, c := range m.connectors {
		if c.Summary != "" {
			parts = append(parts, c.Summary)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}
