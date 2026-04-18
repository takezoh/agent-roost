package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/agent-roost/tui/glyphs"
)

// maxSubtitleLines caps the number of non-empty subtitle lines rendered
// in a session card.
const maxSubtitleLines = 5

func (m Model) View() tea.View {
	width := m.width
	if width <= 0 {
		width = 60
	}

	visible := countSessions(m.items)
	total := len(m.sessions)
	header := titleStyle.Render("SESSIONS") + "  " + badgeStyle.Render(fmt.Sprintf("%d/%d sessions", visible, total))
	filterBar, _ := filterBarLayout(m.filter)
	body := renderSessionsBody(&m, width)
	hintBar := mutedStyle.Render(m.help.ShortHelpView(m.keys.ShortHelp()))

	parts := []string{header}
	if m.workspaceBarVisible() {
		workspaceBar, _ := workspaceBarLayout(m.workspaces, m.selectedWorkspace)
		parts = append(parts, workspaceBar)
	}
	parts = append(parts, filterBar, "")
	if summary := m.connectorSummaryLine(); summary != "" {
		parts = append(parts, "  "+mutedStyle.Render(summary))
	}
	parts = append(parts, body, hintBar)
	screen := lipgloss.JoinVertical(lipgloss.Left, parts...)

	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

// footerRowCount returns the number of rows reserved at the bottom of the
// sidebar for the hint bar.
func (m Model) footerRowCount() int {
	return 1
}

func renderSessionsBody(m *Model, innerWidth int) string {
	if len(m.items) == 0 {
		return mutedStyle.Render("  No sessions")
	}

	// Pass 1: render all items to measure their row heights.
	rendered := make([]string, len(m.items))
	for i := range m.items {
		selected := i == m.cursor
		notif := ""
		if item := m.items[i]; item.session != nil {
			notif = m.latestNotifLine(item.session.ID)
		}
		r := renderItem(m.items[i], selected, innerWidth, m.folded[m.items[i].project], notif)
		if Active.Minimal && i > 0 && !m.items[i].isProject && !m.items[i-1].isProject {
			r = renderSessionSeparator(innerWidth) + "\n" + r
		}
		m.items[i].SetRows(r)
		rendered[i] = r
	}

	// Compute available body height and adjust scroll offset.
	// Reserve rows for the chrome: header area and hint bar footer.
	bodyHeight := m.height - m.headerRowCount() - m.footerRowCount()
	if bodyHeight < 3 {
		bodyHeight = 3
	}
	// Reserve 3 rows for potential scroll chrome (↑ indicator, sticky
	// project header, ↓ indicator) so that ensureCursorVisible never
	// places the cursor outside the visible area.
	m.ensureCursorVisible(bodyHeight - 3)

	// Subtract actual indicator rows from the available item height.
	itemHeight := bodyHeight
	if m.offset > 0 {
		itemHeight-- // "↑ N more"
	}
	sticky := stickyProject(m.items, m.offset)
	if sticky != "" {
		itemHeight-- // sticky project header
	}
	end := m.visibleEnd(itemHeight)
	if end < len(m.items) {
		end = m.visibleEnd(itemHeight - 1) // "↓ N more"
	}

	// Pass 2: assemble visible items with scroll indicators.
	var b strings.Builder
	if m.offset > 0 {
		above := countSessions(m.items[:m.offset])
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ↑ %d more", above)))
		b.WriteString("\n")
	}
	if sticky != "" {
		b.WriteString(renderProject(sticky, "", false, false))
		b.WriteString("\n")
	}
	for i := m.offset; i < end; i++ {
		b.WriteString(rendered[i])
		if i < end-1 {
			b.WriteString("\n")
		}
	}
	if end < len(m.items) {
		below := countSessions(m.items[end:])
		b.WriteString("\n")
		b.WriteString(mutedStyle.Render(fmt.Sprintf("  ↓ %d more", below)))
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

// stickyProject returns the project name that should be shown as a sticky
// header, or "" if none is needed. A sticky header is shown when the
// project header for the first visible item has scrolled out of view.
func stickyProject(items []listItem, offset int) string {
	if offset <= 0 || len(items) == 0 {
		return ""
	}
	proj := items[offset].project
	for i := offset; i < len(items); i++ {
		if items[i].isProject && items[i].project == proj {
			return ""
		}
		if items[i].project != proj {
			break
		}
	}
	return proj
}

func renderItem(item listItem, selected bool, width int, folded bool, notifLine string) string {
	if item.isProject {
		return renderProject(item.project, item.projectPath, folded, selected)
	}
	return renderSession(item.session, selected, width, notifLine)
}

func renderProject(name, path string, folded, selected bool) string {
	arrow := glyphs.Get("fold.open")
	if folded {
		arrow = glyphs.Get("fold.closed")
	}
	label := Link(fileLink(path), name)
	line := fmt.Sprintf("%s %s", arrow, label)
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

func renderSession(s *proto.SessionInfo, selected bool, width int, notifLine string) string {
	if Active.Minimal {
		return renderSessionMinimal(s, selected, width, notifLine)
	}
	cardOuter := width - 2     // leave room for the 2-space indent
	textWidth := cardOuter - 4 // subtract Card border + padding
	body := strings.Join(sessionCardLines(s, textWidth, notifLine), "\n")
	return indent(Card(body, selected, cardOuter, sessionStateIcon(s), s.View.Card.BorderTitle, s.View.Card.BorderTitleSecondary, s.View.Card.BorderBadge), "  ")
}

// renderSessionMinimal draws a session as a borderless block with a
// 1-cell left bar. The bar becomes a Primary-colored "▌" when the card
// is selected and a blank cell otherwise (to keep alignment across all
// cards). No background fill — adjacent sessions are separated by a
// horizontal rule drawn in renderSessionsBody.
func renderSessionMinimal(s *proto.SessionInfo, selected bool, width int, notifLine string) string {
	cardOuter := width - 2     // 2-cell outer indent
	textWidth := cardOuter - 3 // 1 border + 1 left padding + 1 right padding
	icon := sessionStateIcon(s)
	iconCells := lipgloss.Width(icon) + 1 // icon + space
	lines := sessionCardLines(s, textWidth-iconCells, notifLine)
	if len(lines) > 0 {
		lines[0] = icon + " " + lines[0]
	}
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

func sessionStateIcon(s *proto.SessionInfo) string {
	switch s.State {
	case state.StatusRunning:
		return runningSpinnerGlyph()
	case state.StatusWaiting:
		return waitingSpinnerGlyph()
	default:
		return stateStyle(s.State).Render(glyphs.Get(s.State.SymbolKey()))
	}
}

func sessionCardLines(s *proto.SessionInfo, textWidth int, notifLine string) []string {
	title := s.View.Card.Title
	if title == "" {
		title = s.ID[:6]
	}
	titleWidth := textWidth
	if titleWidth < 1 {
		titleWidth = 1
	}
	titleStr := cardTitleStyle.Render(truncate(title, titleWidth))

	lines := []string{titleStr}

	// Subtitle may carry an embedded newline-separated multi-line summary.
	// Split and render each line independently so haiku-generated 2-3 line
	// summaries get a row each instead of a literal "\n" rendered onto one
	// line.
	if subtitle := s.View.Card.Subtitle; subtitle != "" {
		n := 0
		for _, sub := range strings.Split(subtitle, "\n") {
			if sub == "" {
				continue
			}
			lines = append(lines, mutedStyle.Render(truncate(sub, textWidth)))
			n++
			if n >= maxSubtitleLines {
				break
			}
		}
	}

	if chips := renderIndicators(s); chips != "" {
		lines = append(lines, chips)
	}
	if tagsLine := renderTags(s); tagsLine != "" {
		lines = append(lines, tagsLine)
	}
	if notifLine != "" {
		bell := glyphs.Get("notif.bell")
		lines = append(lines, mutedStyle.Render(bell+" "+truncate(notifLine, textWidth-3)))
	}

	return lines
}

// renderTags walks the driver-provided Tags list and renders each one with
// the color the driver chose. The TUI does no special-casing — every tag
// (including the command tag) is identical from the renderer's POV.
func renderTags(s *proto.SessionInfo) string {
	tags := s.View.Card.Tags
	if len(tags) == 0 {
		return ""
	}
	if Active.Minimal {
		var parts []string
		for _, tag := range tags {
			prefix := minimalTagBranchPrefixStyle.Render("⎇")
			parts = append(parts, prefix+" "+minimalTagTextStyle.Render(tag.Text))
		}
		return strings.Join(parts, "  ")
	}
	var parts []string
	for _, tag := range tags {
		parts = append(parts, renderTag(tag))
	}
	return strings.Join(parts, " ")
}

func renderIndicators(s *proto.SessionInfo) string {
	if len(s.View.Card.Indicators) == 0 {
		return ""
	}
	return mutedStyle.Render(strings.Join(s.View.Card.Indicators, "  "))
}

func renderTag(tag state.Tag) string {
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
