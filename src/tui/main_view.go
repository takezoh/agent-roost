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

// stateAnim holds the display definition for one status in an icon scheme.
// If frames has >=2 entries the cell animates (cycling via animFrame); if
// frames has exactly 1 entry it is shown statically; if empty, glyph is used.
type stateAnim struct {
	frames []string
	glyph  string
}

func (a stateAnim) current() string {
	if len(a.frames) > 0 {
		return a.frames[int(animFrame)%len(a.frames)]
	}
	return a.glyph
}

// anim builds an animated stateAnim from a variadic list of frame strings.
func anim(frames ...string) stateAnim { return stateAnim{frames: frames} }

// static builds a non-animated stateAnim from a single glyph.
func static(g string) stateAnim { return stateAnim{glyph: g} }

// iconScheme describes a full icon proposal for all 5 session statuses.
// Index 0=Running, 1=Waiting, 2=Idle, 3=Stopped, 4=Pending (matches Status iota).
type iconScheme struct {
	label  string
	states [5]stateAnim
}

// renderIconPreviewBody renders a comparison table of status icon schemes
// so the user can evaluate all options in-context before committing to one.
// Any status cell with >=2 frames animates live via animFrame.
func renderIconPreviewBody() string {
	statuses := []state.Status{
		state.StatusRunning, state.StatusWaiting, state.StatusIdle,
		state.StatusStopped, state.StatusPending,
	}

	schemes := []iconScheme{
		{
			label: "Unicode",
			states: [5]stateAnim{
				anim(spinner.Pulse.Frames...), // Running: █▓▒░
				static("⋯"),                   // Waiting
				static("⏺"),                   // Idle
				static("⏹"),                   // Stopped
				anim("⚡", "∙"),                // Pending: blink
			},
		},
		{
			label: "Emoji",
			states: [5]stateAnim{
				anim(spinner.Moon.Frames...), // Running: 🌑..🌘
				static("💬"),                  // Waiting
				static("💤"),                  // Idle
				static("⛔"),                  // Stopped
				anim("⚡", "✨"),               // Pending: blink
			},
		},
		{
			label: "NerdFont",
			states: [5]stateAnim{
				anim(nerdSandFrames...),  // Running: timer-sand
				static("\uf141"),         // Waiting: ellipsis-h
				static("\uf04c"),         // Idle: pause
				static("\uf04d"),         // Stopped: stop
				anim("\uf0e7", "∙"),      // Pending: bolt blink
			},
		},
		{
			label: "Spinner",
			states: [5]stateAnim{
				anim(spinner.Hamburger.Frames...), // Running: ☱☲☴☲
				static("◆"),                        // Waiting (current)
				static("○"),                        // Idle (current)
				static("■"),                        // Stopped (current)
				static("◇"),                        // Pending (current)
			},
		},
	}

	labelW := lipgloss.NewStyle().Width(10)
	cellW := lipgloss.NewStyle().Width(9).Align(lipgloss.Center)
	stateLabelW := lipgloss.NewStyle().Width(9)

	// STATUS ICON PREVIEW table
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
		for i, st := range statuses {
			cells = append(cells, cellW.Render(stateStyle(st).Render(sc.states[i].current())))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Left, cells...))
	}
	rows = append(rows, mutedStyle.Render("  NerdFont row requires a Nerd Font"))

	// SPINNER FRAMES: one row per scheme per animated state.
	rows = append(rows, "", sectionStyle.Render("SPINNER FRAMES"))
	for _, sc := range schemes {
		for i, st := range statuses {
			if len(sc.states[i].frames) < 2 {
				continue
			}
			var parts []string
			for _, f := range sc.states[i].frames {
				parts = append(parts, stateStyle(st).Render(f))
			}
			left := labelW.Render(mutedStyle.Render(sc.label))
			stLabel := stateLabelW.Render(mutedStyle.Render(st.String()))
			rows = append(rows, left+stLabel+strings.Join(parts, " "))
		}
	}

	return strings.Join(rows, "\n")
}
