package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/tools"
)

func (m PaletteModel) View() tea.View {
	outerWidth := m.width
	if outerWidth <= 0 || outerWidth > 80 {
		outerWidth = 60
	}

	innerWidth := outerWidth - 4
	var body, title, badge string

	switch m.phase {
	case phaseToolSelect:
		title = "PALETTE"
		badge = fmt.Sprintf("%d tools", len(m.filtered))
		body = renderPaletteTool(m, innerWidth)
	case phaseParamSelect:
		if m.selectedTool != nil && m.paramIndex < len(m.selectedTool.Params) {
			p := m.selectedTool.Params[m.paramIndex]
			title = m.selectedTool.Name
			badge = p.Name
			body = renderPaletteParam(m, innerWidth)
		}
	}

	return tea.NewView(Panel(title, badge, body, outerWidth))
}

func renderPaletteTool(m PaletteModel, innerWidth int) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

	total := len(m.filtered)
	maxVisible := m.height - PanelChromeRows - 2
	start, end := 0, total
	if maxVisible >= 3 && total > maxVisible {
		start, end = visibleWindow(m.cursor, total, maxVisible-2)
	}

	if start > 0 {
		b.WriteString(descStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		t := m.filtered[i]
		desc := descStyle.Render("  " + t.Description)
		line := fmt.Sprintf("  %s", t.Name) + desc
		if i == m.cursor {
			line = fmt.Sprintf("▸ %s", t.Name) + desc
			b.WriteString(selItemStyle.Width(innerWidth).MaxHeight(1).Render(line))
		} else {
			b.WriteString(itemStyle.Width(innerWidth).MaxHeight(1).Render(line))
		}
		if i < end-1 || end < total {
			b.WriteString("\n")
		}
	}
	if end < total {
		b.WriteString(descStyle.Render(fmt.Sprintf("  ↓ %d more", total-end)))
	}
	if total == 0 {
		b.WriteString(descStyle.Render("(no matching tools)"))
	}
	return b.String()
}

func paramOptionSuffix(raw string) string {
	dir := filepath.Dir(raw)
	if dir == "." {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + dir[len(home):]
	}
	return descStyle.Render("  " + dir)
}

func renderPaletteParam(m PaletteModel, innerWidth int) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

	if m.selectedTool != nil && m.selectedTool.Name == "new-session" &&
		m.paramIndex < len(m.selectedTool.Params) &&
		m.selectedTool.Params[m.paramIndex].Name == "command" {
		stateText := "off"
		if m.worktreeOn {
			stateText = "on"
		}
		b.WriteString(selItemStyle.Render(" worktree " + stateText + " "))
		b.WriteString(descStyle.Render("  Tab to toggle"))
		b.WriteString("\n\n")
	}

	if len(m.paramOptions) == 0 {
		b.WriteString(descStyle.Render("(type value, enter to confirm)"))
		return b.String()
	}
	filtered := m.filterParamOptions()
	total := len(filtered)
	maxVisible := m.height - PanelChromeRows - 2
	start, end := 0, total
	if maxVisible >= 3 && total > maxVisible {
		start, end = visibleWindow(m.paramCursor, total, maxVisible-2)
	}

	if start > 0 {
		b.WriteString(descStyle.Render(fmt.Sprintf("  ↑ %d more", start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		display := tools.ProjectDisplayName(filtered[i])
		suffix := paramOptionSuffix(filtered[i])
		line := fmt.Sprintf("  %s", display) + suffix
		if i == m.paramCursor {
			line = fmt.Sprintf("▸ %s", display) + suffix
			b.WriteString(selItemStyle.Width(innerWidth).MaxHeight(1).Render(line))
		} else {
			b.WriteString(itemStyle.Width(innerWidth).MaxHeight(1).Render(line))
		}
		if i < end-1 || end < total {
			b.WriteString("\n")
		}
	}
	if end < total {
		b.WriteString(descStyle.Render(fmt.Sprintf("  ↓ %d more", total-end)))
	}
	if total == 0 {
		b.WriteString(descStyle.Render("(no matching items)"))
	}
	return b.String()
}
