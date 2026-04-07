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
	pendingStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff8800"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	tagStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
)

func (m Model) View() tea.View {
	var b strings.Builder

	b.WriteString(titleStyle.Render("SESSIONS"))
	b.WriteString("\n\n")

	for i := range m.items {
		selected := i == m.cursor
		rendered := renderItem(m.items[i], selected, m.width, m.folded[m.items[i].project], m.drivers)
		m.items[i].SetRows(rendered)
		b.WriteString(rendered)
		b.WriteString("\n")
	}

	if len(m.items) == 0 {
		b.WriteString(idleStyle.Render("  No sessions"))
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
	style := stateStyle(s.State)
	stateStr := style.Render(s.State.Symbol() + " " + s.State.String())
	elapsed := formatElapsed(time.Since(s.StateChangedAtTime()))

	name := s.ID[:6]
	if s.Title != "" {
		name = truncate(s.Title, 28)
	}
	line1 := fmt.Sprintf("  %s %s  %s", name, stateStr, elapsed)

	content := line1
	if s.LastPrompt != "" {
		content += "\n  " + idleStyle.Render(truncate(s.LastPrompt, 30))
	}

	const maxDisplaySubjects = 5
	subjects := s.Subjects
	if len(subjects) > maxDisplaySubjects {
		subjects = subjects[len(subjects)-maxDisplaySubjects:]
	}
	for _, subj := range subjects {
		content += "\n    " + idleStyle.Render("• "+truncate(subj, 26))
	}

	displayName := registry.Get(s.Command).DisplayName()
	var tagParts []string
	tagParts = append(tagParts, tagStyle.Render("["+displayName+"]"))
	for _, tag := range s.Tags {
		tagParts = append(tagParts, renderTag(tag))
	}
	content += "\n  " + strings.Join(tagParts, " ")
	if selected {
		return selectedStyle.Render(content)
	}
	return content
}

func renderTag(tag session.Tag) string {
	style := lipgloss.NewStyle()
	if tag.Foreground != "" {
		style = style.Foreground(lipgloss.Color(tag.Foreground))
	}
	if tag.Background != "" {
		style = style.Background(lipgloss.Color(tag.Background))
	}
	return style.Render("[" + tag.Text + "]")
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
	case session.StatePending:
		return pendingStyle
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
