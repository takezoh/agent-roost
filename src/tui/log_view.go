package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	activeTabStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	inactiveTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	logWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	logErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	logDebugStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	followStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
)

func (m LogModel) View() tea.View {
	var b strings.Builder

	var appLabel, sessionLabel string
	if m.activeTab == tabApp {
		appLabel = activeTabStyle.Render("[App]")
		sessionLabel = inactiveTabStyle.Render(" Session")
	} else {
		appLabel = inactiveTabStyle.Render(" App ")
		sessionLabel = activeTabStyle.Render("[Session]")
	}
	header := appLabel + sessionLabel

	if m.following {
		header += " " + followStyle.Render("↓")
	} else {
		header += " " + logDebugStyle.Render(fmt.Sprintf("%.0f%%", m.viewport.ScrollPercent()*100))
	}
	b.WriteString(header)
	b.WriteString("\n")

	if m.activeTab == tabSession && m.sessionLogPath == "" {
		b.WriteString(inactiveTabStyle.Render("  セッションなし"))
	} else {
		b.WriteString(m.viewport.View())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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
