package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/take/agent-roost/core"
)

var (
	mainTitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	mainKeyStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#EBDBB2"))
	mainDescStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	mainSectionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
)

func (m MainModel) View() tea.View {
	var b strings.Builder

	b.WriteString(mainTitleStyle.Render("roost"))
	b.WriteString("\n\n")

	renderKeybindings(&b)

	if name := m.selectedProjectName(); name != "" {
		b.WriteString("\n")
		b.WriteString(mainSectionStyle.Render("─── " + name + " ───"))
		b.WriteString("\n\n")
		renderProjectSessions(&b, m.projectSessions())
	}

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func renderKeybindings(b *strings.Builder) {
	bindings := []struct{ key, desc string }{
		{"prefix+Space", "Toggle TUI"},
		{"prefix+p", "Palette"},
		{"prefix+d", "Detach"},
		{"prefix+q", "Shutdown"},
	}
	for _, bind := range bindings {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			mainKeyStyle.Render(fmt.Sprintf("%-14s", bind.key)),
			mainDescStyle.Render(bind.desc),
		))
	}
}

func renderProjectSessions(b *strings.Builder, sessions []core.SessionInfo) {
	if len(sessions) == 0 {
		b.WriteString(mainDescStyle.Render("  セッションなし"))
		b.WriteString("\n")
		return
	}
	for _, s := range sessions {
		symbol := stateSymbol(s.State)
		elapsed := formatElapsed(time.Since(s.CreatedAtTime()))
		b.WriteString(fmt.Sprintf("  %s  %s %-5s /%s\n",
			s.ID[:6], symbol, elapsed, s.DisplayCommand()))
	}
}
