package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func (m MainModel) View() tea.View {
	title := titleStyle.Render("ROOST")
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, title, m.viewport.View()))
	v.AltScreen = true
	return v
}

func (m MainModel) renderContent() string {
	parts := []string{
		"",
		renderKeybindingsBody(),
	}

	for _, c := range m.connectors {
		if !c.Available || len(c.Sections) == 0 {
			continue
		}
		parts = append(parts, "", projectStyle.Render(c.Label))
		parts = append(parts, renderConnectorSections(c.Sections)...)
	}

	if name := m.selectedProjectName(); name != "" {
		sessions := m.projectSessions()
		header := projectStyle.Render(name) + "  " + badgeStyle.Render(fmt.Sprintf("%d sessions", len(sessions)))
		parts = append(parts, "", header, "", renderProjectSessionsBody(sessions))
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func renderKeybindingsBody() string {
	bindings := []struct{ key, desc string }{
		{"prefix+Space", "Toggle TUI"},
		{"prefix+p", "Palette"},
		{"prefix+d", "Detach"},
		{"prefix+q", "Shutdown"},
	}
	var b strings.Builder
	for i, bind := range bindings {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("%s  %s",
			helpKeyStyle.Render(fmt.Sprintf("%-14s", bind.key)),
			mutedStyle.Render(bind.desc),
		))
	}
	return b.String()
}

func renderConnectorSections(sections []state.ConnectorSection) []string {
	var parts []string
	for _, sec := range sections {
		parts = append(parts, mutedStyle.Render(sec.Title))
		for _, item := range sec.Items {
			parts = append(parts, fmt.Sprintf("%s %s  %s",
				mutedStyle.Render(item.Symbol),
				item.Title,
				mutedStyle.Render(item.Meta),
			))
		}
	}
	return parts
}

func renderProjectSessionsBody(sessions []proto.SessionInfo) string {
	if len(sessions) == 0 {
		return mutedStyle.Render("No sessions")
	}
	var b strings.Builder
	for i, s := range sessions {
		if i > 0 {
			b.WriteString("\n")
		}
		symbol := stateSymbol(s.State)
		elapsed := formatElapsed(time.Since(s.CreatedAtTime()))
		tagText := s.View.DisplayName
		if tagText == "" {
			tagText = "?"
		}
		b.WriteString(fmt.Sprintf("%s  %s %s  %s",
			mutedStyle.Render(s.ID[:6]),
			symbol,
			mutedStyle.Render(fmt.Sprintf("%-5s", elapsed)),
			tagStyle.Render(tagText),
		))
	}
	return b.String()
}
