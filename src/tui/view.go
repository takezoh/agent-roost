package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
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
		// In minimal mode, draw a horizontal separator between two adjacent
		// sessions. The separator is prepended to the lower card so that
		// SetRows accounts for it and mouse mapping clicks the lower card.
		if Active.Minimal && i > 0 && !m.items[i].isProject && !m.items[i-1].isProject {
			rendered = renderSessionSeparator(innerWidth) + "\n" + rendered
		}
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
	if Active.Minimal {
		if selected {
			return minimalProjectSelStyle.Render("▌ " + line)
		}
		return "  " + projectStyle.Render(line)
	}
	if selected {
		return selectedStyle.Render(line)
	}
	return projectStyle.Render(line)
}

func renderSession(s *core.SessionInfo, selected bool, width int, registry *driver.Registry) string {
	if Active.Minimal {
		return renderSessionMinimal(s, selected, width, registry)
	}
	cardOuter := width - 2     // leave room for the 2-space indent
	textWidth := cardOuter - 4 // subtract Card border + padding
	body := strings.Join(sessionCardLines(s, textWidth, registry), "\n")
	return indent(Card(body, selected, cardOuter), "  ")
}

// renderSessionMinimal draws a session as a borderless block with a
// 1-cell left bar. The bar becomes a Primary-colored "▌" when the card
// is selected and a blank cell otherwise (to keep alignment across all
// cards). No background fill — adjacent sessions are separated by a
// horizontal rule drawn in renderSessionsBody.
func renderSessionMinimal(s *core.SessionInfo, selected bool, width int, registry *driver.Registry) string {
	cardOuter := width - 2     // 2-cell outer indent
	textWidth := cardOuter - 3 // 1 border + 1 left padding + 1 right padding
	lines := sessionCardLines(s, textWidth, registry)
	body := strings.Join(lines, "\n")

	barChar := " "
	if selected {
		barChar = "▌"
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.Border{Left: barChar}, false, false, false, true).
		BorderForeground(Active.Primary).
		Width(cardOuter).
		Padding(0, 1)

	return indent(style.Render(body), "  ")
}

// renderSessionSeparator returns a single-line horizontal rule used to
// visually divide two adjacent session cards in minimal mode. Indented
// by 2 spaces to align with the cards above and below.
func renderSessionSeparator(innerWidth int) string {
	n := innerWidth - 2
	if n < 1 {
		n = 1
	}
	return "  " + minimalSeparatorStyle.Render(strings.Repeat("─", n))
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
	titleStr := cardTitleStyle.Render(truncate(title, titleWidth))

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
	if Active.Minimal {
		driverPrefix := minimalTagDriverPrefixStyle.Render("▸")
		branchPrefix := minimalTagBranchPrefixStyle.Render("⎇")
		var parts []string
		parts = append(parts, driverPrefix+" "+minimalTagTextStyle.Render(displayName))
		for _, tag := range s.Tags {
			parts = append(parts, branchPrefix+" "+minimalTagTextStyle.Render(tag.Text))
		}
		return strings.Join(parts, "  ")
	}
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
