package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// sessionsHeaderRows is the number of rendered rows before the first list
// item inside the Sessions view (header line + filter bar + blank).
// The mouse row→item mapping relies on this staying in sync with View().
const sessionsHeaderRows = 3

func (m Model) View() tea.View {
	width := m.width
	if width <= 0 {
		width = 60
	}

	visible := countSessions(m.items)
	total := len(m.sessions)
	header := titleStyle.Render("SESSIONS") + "  " + badgeStyle.Render(fmt.Sprintf("%d/%d sessions", visible, total))
	filterBar, _ := filterBarLayout(m.filter)
	body := renderSessionsBody(m, width)
	footer := Footer([]key.Binding{m.keys.New, m.keys.NewCmd, m.keys.Enter, m.keys.Stop, m.keys.Toggle, m.keys.FilterHelp})

	screen := lipgloss.JoinVertical(lipgloss.Left, header, filterBar, "", body, "", footer)

	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func renderSessionsBody(m Model, innerWidth int) string {
	if len(m.items) == 0 {
		return mutedStyle.Render("  No sessions")
	}

	var b strings.Builder
	for i := range m.items {
		selected := i == m.cursor
		rendered := renderItem(m.items[i], selected, innerWidth, m.folded[m.items[i].project], m.drivers)
		m.items[i].SetRows(rendered)
		b.WriteString(rendered)
		if i < len(m.items)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func countSessions(items []listItem) int {
	n := 0
	for _, it := range items {
		if !it.isProject {
			n++
		}
	}
	return n
}

func renderItem(item listItem, selected bool, width int, folded bool, registry *driver.Registry) string {
	if item.isProject {
		return renderProject(item.project, folded, selected)
	}
	return renderSession(item.session, selected, width, registry)
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

func renderSession(s *core.SessionInfo, selected bool, width int, registry *driver.Registry) string {
	cardOuter := width - 2     // leave room for the 2-space indent
	textWidth := cardOuter - 4 // subtract Card border + padding
	body := strings.Join(sessionCardLines(s, textWidth, registry), "\n")
	return indent(Card(body, selected, cardOuter), "  ")
}

func sessionCardLines(s *core.SessionInfo, textWidth int, registry *driver.Registry) []string {
	stateStr := stateStyle(s.State).Render(s.State.Symbol() + " " + s.State.String())
	elapsed := mutedStyle.Render(formatElapsed(time.Since(s.StateChangedAtTime())))

	title := s.Title
	if title == "" {
		title = s.ID[:6]
	}
	prefix := stateStr + "  " + elapsed + "  "
	titleWidth := textWidth - lipgloss.Width(prefix)
	if titleWidth < 1 {
		titleWidth = 1
	}
	titleStr := lipgloss.NewStyle().Foreground(DefaultTheme.Fg).Render(truncate(title, titleWidth))

	lines := []string{prefix + titleStr}

	if s.LastPrompt != "" {
		lines = append(lines, mutedStyle.Render(truncate(s.LastPrompt, textWidth)))
	}

	const maxDisplaySubjects = 3
	subjects := s.Subjects
	if len(subjects) > maxDisplaySubjects {
		subjects = subjects[len(subjects)-maxDisplaySubjects:]
	}
	for _, subj := range subjects {
		lines = append(lines, mutedStyle.Render("• "+truncate(subj, textWidth-2)))
	}

	if chips := renderIndicators(s); chips != "" {
		lines = append(lines, chips)
	}
	if tagsLine := renderTags(s, registry); tagsLine != "" {
		lines = append(lines, tagsLine)
	}
	return lines
}

func renderTags(s *core.SessionInfo, registry *driver.Registry) string {
	displayName := registry.Get(s.Command).DisplayName()
	var parts []string
	parts = append(parts, tagStyle.Render(displayName))
	for _, tag := range s.Tags {
		parts = append(parts, renderTag(tag))
	}
	return strings.Join(parts, " ")
}

func renderIndicators(s *core.SessionInfo) string {
	if len(s.Indicators) == 0 {
		return ""
	}
	return mutedStyle.Render(strings.Join(s.Indicators, "  "))
}

func renderTag(tag session.Tag) string {
	style := tagStyle
	if tag.Foreground != "" {
		style = style.Foreground(lipgloss.Color(tag.Foreground))
	}
	if tag.Background != "" {
		style = style.Background(lipgloss.Color(tag.Background))
	}
	return style.Render(tag.Text)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if n <= 0 || len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func indent(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
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
