package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"
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
		"",
		renderIconPreviewBody(),
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

// nerdSandFrames is a 4-frame timer-sand animation using Nerd Font PUA
// codepoints (nf-md-timer-sand-empty through nf-md-timer-sand-full).
// Renders as hourglass stages when a Nerd Font is installed; falls back to
// replacement characters otherwise.
var nerdSandFrames = []string{"\uf251", "\uf252", "\uf253", "\uf254"}

// renderIconPreviewBody renders a comparison table of status icon schemes
// so the user can evaluate all options in-context before committing to one.
// Running cells animate using per-scheme spinner presets.
func renderIconPreviewBody() string {
	statuses := []state.Status{
		state.StatusRunning, state.StatusWaiting, state.StatusIdle,
		state.StatusStopped, state.StatusPending,
	}

	makeAnim := func(frames []string) func() string {
		return func() string {
			return stateStyle(state.StatusRunning).Render(frames[int(animFrame)%len(frames)])
		}
	}

	type scheme struct {
		label   string
		running func() string
		glyphs  [4]string // Waiting, Idle, Stopped, Pending
	}
	schemes := []scheme{
		{"Current", runningSpinnerGlyph, [4]string{"◆", "○", "■", "◇"}},
		{"Unicode⁺", makeAnim(spinner.Pulse.Frames), [4]string{"⏸", "⏺", "⏹", "⊘"}},
		{"Emoji", makeAnim(spinner.Moon.Frames), [4]string{"🟡", "⚪", "🔴", "🟠"}},
		{"NerdFont", makeAnim(nerdSandFrames), [4]string{"\uf04c", "\uf111", "\uf04d", "\uf017"}},
	}

	labelW := lipgloss.NewStyle().Width(10)
	cellW := lipgloss.NewStyle().Width(9).Align(lipgloss.Center)

	// Header row
	header := []string{labelW.Render("")}
	for _, st := range statuses {
		header = append(header, cellW.Render(mutedStyle.Render(st.String())))
	}

	rows := []string{
		sectionStyle.Render("STATUS ICON PREVIEW"),
		lipgloss.JoinHorizontal(lipgloss.Left, header...),
	}
	for _, sc := range schemes {
		cells := []string{labelW.Render(mutedStyle.Render(sc.label))}
		cells = append(cells, cellW.Render(sc.running()))
		for i, g := range sc.glyphs {
			cells = append(cells, cellW.Render(stateStyle(statuses[i+1]).Render(g)))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, cells...))
	}
	rows = append(rows, mutedStyle.Render("  NerdFont row requires a Nerd Font"))

	// Spinner frames preview: show all frames of each Running animation inline.
	type spinnerDef struct {
		label  string
		frames []string
	}
	spinners := []spinnerDef{
		{"Current", spinner.MiniDot.Frames},
		{"Unicode⁺", spinner.Pulse.Frames},
		{"Emoji", spinner.Moon.Frames},
		{"NerdFont", nerdSandFrames},
	}
	rows = append(rows, "", sectionStyle.Render("SPINNER FRAMES"))
	runStyle := stateStyle(state.StatusRunning)
	for _, sp := range spinners {
		var parts []string
		for _, f := range sp.frames {
			parts = append(parts, runStyle.Render(f))
		}
		rows = append(rows, labelW.Render(mutedStyle.Render(sp.label))+strings.Join(parts, " "))
	}

	return strings.Join(rows, "\n")
}
