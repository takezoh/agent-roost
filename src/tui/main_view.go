package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/session/driver"
)

func (m MainModel) View() tea.View {
	parts := []string{
		titleStyle.Render("ROOST"),
		"",
		renderKeybindingsBody(),
	}

	if name := m.selectedProjectName(); name != "" {
		sessions := m.projectSessions()
		header := projectStyle.Render(name) + "  " + badgeStyle.Render(fmt.Sprintf("%d sessions", len(sessions)))
		parts = append(parts, "", header, "", renderProjectSessionsBody(sessions, m.drivers))
	}

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, parts...))
	v.AltScreen = true
	return v
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

func renderProjectSessionsBody(sessions []core.SessionInfo, registry *driver.Registry) string {
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
		displayName := registry.Get(s.Command).DisplayName()
		b.WriteString(fmt.Sprintf("%s  %s %s  %s",
			mutedStyle.Render(s.ID[:6]),
			symbol,
			mutedStyle.Render(fmt.Sprintf("%-5s", elapsed)),
			tagStyle.Render("/"+displayName),
		))
	}
	return b.String()
}
