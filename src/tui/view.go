package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	projectStyle  = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("#3C3836")).Foreground(lipgloss.Color("#EBDBB2"))
	runningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ff00"))
	waitingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffff00"))
	idleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	stoppedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

func (m Model) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render("SESSIONS"))
	b.WriteString("\n\n")

	for i, item := range m.items {
		selected := i == m.cursor
		b.WriteString(renderItem(item, selected, m.width, m.folded[item.project]))
		b.WriteString("\n")
	}

	if len(m.items) == 0 {
		b.WriteString(idleStyle.Render("  セッションなし"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(renderHelp(m.keys))

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func renderItem(item listItem, selected bool, width int, folded bool) string {
	if item.isProject {
		return renderProject(item.project, folded, selected)
	}
	return renderSession(item.session, selected)
}

func renderProject(name string, folded, selected bool) string {
	arrow := "▼"
	if folded {
		arrow = "▶"
	}
	line := fmt.Sprintf("%s %s", arrow, name)
	if selected {
		return selectedStyle.Render(line)
	}
	return projectStyle.Render(line)
}

func renderSession(s *core.SessionInfo, selected bool) string {
	symbol := stateStyle(s.State).Render(s.State.Symbol())
	elapsed := formatElapsed(time.Since(s.CreatedAtTime()))

	line1 := fmt.Sprintf("  %s %s  %s", s.ID[:6], symbol, elapsed)
	line2 := fmt.Sprintf("    /%s", s.DisplayCommand())

	content := line1 + "\n" + line2
	if selected {
		return selectedStyle.Render(content)
	}
	return content
}

func stateStyle(s session.State) lipgloss.Style {
	switch s {
	case session.StateRunning:
		return runningStyle
	case session.StateWaiting:
		return waitingStyle
	case session.StateIdle:
		return idleStyle
	case session.StateStopped:
		return stoppedStyle
	default:
		return idleStyle
	}
}

func formatElapsed(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func renderHelp(keys KeyMap) string {
	items := []string{
		keys.New.Help().Key + ":" + keys.New.Help().Desc,
		keys.NewCmd.Help().Key + ":" + keys.NewCmd.Help().Desc,
		keys.Enter.Help().Key + ":" + keys.Enter.Help().Desc,
		keys.Stop.Help().Key + ":" + keys.Stop.Help().Desc,
	}
	return helpStyle.Render(strings.Join(items, "  "))
}
