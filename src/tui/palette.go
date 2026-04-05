package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

type PaletteModel struct {
	registry *Registry
	ctx      *ToolContext
	input    string
	filtered []Tool
	cursor   int
	width    int
	height   int
	err      error
}

func NewPaletteModel(registry *Registry, ctx *ToolContext) PaletteModel {
	return PaletteModel{
		registry: registry,
		ctx:      ctx,
		filtered: registry.All(),
	}
}

func (m PaletteModel) Init() tea.Cmd {
	return nil
}

func (m PaletteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

var (
	escapeBinding = key.NewBinding(key.WithKeys("escape"))
	enterBinding  = key.NewBinding(key.WithKeys("enter"))
	upBinding     = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	downBinding   = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	bsBinding     = key.NewBinding(key.WithKeys("backspace"))
)

func (m PaletteModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, escapeBinding):
		return m, tea.Quit
	case key.Matches(msg, enterBinding):
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			tool := m.filtered[m.cursor]
			if err := tool.Run(m.ctx); err != nil {
				m.err = err
			}
			return m, tea.Quit
		}
	case key.Matches(msg, upBinding):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, downBinding):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case key.Matches(msg, bsBinding):
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			m.refilter()
		}
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= 0x20 && s[0] < 0x7f {
			m.input += s
			m.refilter()
		}
	}
	return m, nil
}

func (m *PaletteModel) refilter() {
	m.filtered = m.registry.Match(m.input)
	m.cursor = 0
}

var (
	paletteBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	itemStyle     = lipgloss.NewStyle()
	selItemStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#3C3836")).Foreground(lipgloss.Color("#EBDBB2"))
	descStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	inputStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#EBDBB2"))
)

func (m PaletteModel) View() tea.View {
	var b strings.Builder

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

	for i, t := range m.filtered {
		name := t.Name
		desc := descStyle.Render(" " + t.Description)
		line := name + desc
		if i == m.cursor {
			line = selItemStyle.Render(fmt.Sprintf("▸ %s", t.Name)) + desc
		} else {
			line = itemStyle.Render(fmt.Sprintf("  %s", t.Name)) + desc
		}
		b.WriteString(line + "\n")
	}

	if len(m.filtered) == 0 {
		b.WriteString(descStyle.Render("  (一致するツールなし)\n"))
	}

	return tea.NewView(paletteBorder.Render(b.String()))
}
