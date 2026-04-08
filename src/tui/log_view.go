package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func (m LogModel) View() tea.View {
	var status string
	if m.following {
		status = followStyle.Render("↓")
	} else {
		status = mutedStyle.Render(fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100))
	}
	header := m.renderTabHeader() + "  " + status

	var body string
	if len(m.tabs) == 0 {
		body = mutedStyle.Render("No sessions")
	} else {
		body = m.viewport.View()
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header, body))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m LogModel) renderTabHeader() string {
	var b strings.Builder
	for i, tab := range m.tabs {
		if i > 0 {
			b.WriteString(" ")
		}
		if logTab(i) == m.activeTab {
			b.WriteString(activeTabStyle.Render("[" + tab.label + "]"))
		} else {
			b.WriteString(inactiveTabStyle.Render(tab.label))
		}
	}
	return b.String()
}

func colorizeLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = colorizeLogLine(line)
	}
	return strings.Join(lines, "\n")
}

func colorizeLogLine(line string) string {
	level := parseLogLevel(line)
	switch level {
	case "ERROR":
		return logErrorStyle.Render(line)
	case "WARN":
		return logWarnStyle.Render(line)
	case "DEBUG":
		return logDebugStyle.Render(line)
	default:
		return line
	}
}

func parseLogLevel(line string) string {
	idx := strings.Index(line, "level=")
	if idx < 0 {
		return ""
	}
	rest := line[idx+6:]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		return rest
	}
	return rest[:end]
}
