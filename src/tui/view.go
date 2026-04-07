package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
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
	tagStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
)

func (m Model) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render("SESSIONS"))
	b.WriteString("\n\n")

	cursorSession := m.cursorSession()
	for _, item := range m.items {
		selected := !item.isProject && cursorSession != nil && item.session == cursorSession
		b.WriteString(renderItem(item, selected, m.width, m.folded[item.project], m.drivers))
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
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func renderItem(item listItem, selected bool, width int, folded bool, registry *driver.Registry) string {
	if item.isProject {
		return renderProject(item.project, folded, selected)
	}
	return renderSession(item.session, selected, registry)
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

func renderSession(s *core.SessionInfo, selected bool, registry *driver.Registry) string {
	symbol := stateStyle(s.State).Render(s.State.Symbol())
	elapsed := formatElapsed(time.Since(s.CreatedAtTime()))

	name := s.ID[:6]
	if s.Title != "" {
		name = truncate(s.Title, 28)
	}
	line1 := fmt.Sprintf("  %s %s  %s", name, symbol, elapsed)

	displayName := registry.Get(s.Command).DisplayName()
	tags := "[" + displayName + "]"
	if s.GitBranch != "" {
		tags += " [" + s.GitBranch + "]"
	}
	line2 := "  " + tagStyle.Render(tags)

	content := line1 + "\n" + line2
	if selected {
		return selectedStyle.Render(content)
	}
	return content
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
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
