package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/agent-roost/core"
)

type palettePhase int

const (
	phaseToolSelect palettePhase = iota
	phaseParamSelect
)

type PaletteModel struct {
	registry    *core.ToolRegistry
	ctx         *core.ToolContext
	initialTool string
	phase       palettePhase
	input       string
	filtered    []core.Tool
	cursor      int

	// parameter input
	selectedTool *core.Tool
	paramIndex   int
	paramArgs    map[string]string
	paramOptions []string
	paramCursor  int

	width  int
	height int
	err    error
}

func NewPaletteModel(registry *core.ToolRegistry, ctx *core.ToolContext, initialTool string) PaletteModel {
	m := PaletteModel{
		registry:    registry,
		ctx:         ctx,
		filtered:    registry.All(),
		paramArgs:   make(map[string]string),
		initialTool: initialTool,
	}
	return m
}

type startToolMsg struct{}

func (m PaletteModel) Init() tea.Cmd {
	if m.initialTool != "" {
		return func() tea.Msg { return startToolMsg{} }
	}
	return nil
}

func (m PaletteModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case startToolMsg:
		tool := m.registry.Get(m.initialTool)
		if tool != nil {
			return m.startTool(tool)
		}
		return m, tea.Quit
	case tea.KeyPressMsg:
		switch m.phase {
		case phaseToolSelect:
			return m.handleToolSelect(msg)
		case phaseParamSelect:
			return m.handleParamSelect(msg)
		}
	}
	return m, nil
}

var (
	escapeBinding = key.NewBinding(key.WithKeys("esc", "escape"))
	enterBinding  = key.NewBinding(key.WithKeys("enter"))
	upBinding     = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	downBinding   = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	bsBinding     = key.NewBinding(key.WithKeys("backspace"))
)

func (m PaletteModel) handleToolSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, escapeBinding):
		return m, tea.Quit
	case key.Matches(msg, enterBinding):
		if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
			tool := m.filtered[m.cursor]
			return m.startTool(&tool)
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

func (m PaletteModel) startTool(tool *core.Tool) (tea.Model, tea.Cmd) {
	m.selectedTool = tool
	m.paramArgs = make(map[string]string)

	// pre-fill from context
	if m.ctx.Args != nil {
		for k, v := range m.ctx.Args {
			m.paramArgs[k] = v
		}
	}

	return m.advanceParam()
}

func (m PaletteModel) advanceParam() (tea.Model, tea.Cmd) {
	for m.paramIndex < len(m.selectedTool.Params) {
		p := m.selectedTool.Params[m.paramIndex]
		if _, filled := m.paramArgs[p.Name]; filled {
			m.paramIndex++
			continue
		}
		// show prompt for this param
		m.phase = phaseParamSelect
		m.paramOptions = p.Options(m.ctx)
		m.paramCursor = 0
		m.input = ""
		return m, nil
	}

	// all params filled, execute
	err := m.selectedTool.Run(m.ctx, m.paramArgs)
	if err != nil {
		slog.Error("tool execution failed", "tool", m.selectedTool.Name, "args", m.paramArgs, "err", err)
	} else {
		slog.Info("tool executed", "tool", m.selectedTool.Name, "args", m.paramArgs)
	}
	return m, tea.Quit
}

func (m PaletteModel) handleParamSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, escapeBinding):
		if m.initialTool != "" {
			return m, tea.Quit
		}
		m.phase = phaseToolSelect
		m.selectedTool = nil
		m.paramIndex = 0
		m.input = ""
		m.refilter()
		return m, nil
	case key.Matches(msg, enterBinding):
		filtered := m.filterParamOptions()
		if len(filtered) > 0 && m.paramCursor < len(filtered) {
			p := m.selectedTool.Params[m.paramIndex]
			m.paramArgs[p.Name] = filtered[m.paramCursor]
			m.paramIndex++
			return m.advanceParam()
		}
	case key.Matches(msg, upBinding):
		if m.paramCursor > 0 {
			m.paramCursor--
		}
	case key.Matches(msg, downBinding):
		filtered := m.filterParamOptions()
		if m.paramCursor < len(filtered)-1 {
			m.paramCursor++
		}
	case key.Matches(msg, bsBinding):
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
			m.paramCursor = 0
		}
	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= 0x20 && s[0] < 0x7f {
			m.input += s
			m.paramCursor = 0
		}
	}
	return m, nil
}

func (m PaletteModel) filterParamOptions() []string {
	if m.input == "" {
		return m.paramOptions
	}
	q := strings.ToLower(m.input)
	var matched []string
	for _, o := range m.paramOptions {
		if strings.Contains(strings.ToLower(o), q) ||
			strings.Contains(strings.ToLower(core.ProjectDisplayName(o)), q) {
			matched = append(matched, o)
		}
	}
	return matched
}

func (m *PaletteModel) refilter() {
	m.filtered = m.registry.Match(m.input)
	m.cursor = 0
}

func (m PaletteModel) View() tea.View {
	outerWidth := m.width
	if outerWidth <= 0 || outerWidth > 80 {
		outerWidth = 60
	}

	var body string
	var title, badge string

	switch m.phase {
	case phaseToolSelect:
		title = "PALETTE"
		badge = fmt.Sprintf("%d tools", len(m.filtered))
		body = renderPaletteTool(m)

	case phaseParamSelect:
		p := m.selectedTool.Params[m.paramIndex]
		title = m.selectedTool.Name
		badge = p.Name
		body = renderPaletteParam(m)
	}

	return tea.NewView(Panel(title, badge, body, outerWidth))
}

func renderPaletteTool(m PaletteModel) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")
	for i, t := range m.filtered {
		desc := descStyle.Render("  " + t.Description)
		if i == m.cursor {
			b.WriteString(selItemStyle.Render(fmt.Sprintf("▸ %s", t.Name)) + desc)
		} else {
			b.WriteString(itemStyle.Render(fmt.Sprintf("  %s", t.Name)) + desc)
		}
		if i < len(m.filtered)-1 {
			b.WriteString("\n")
		}
	}
	if len(m.filtered) == 0 {
		b.WriteString(descStyle.Render("(no matching tools)"))
	}
	return b.String()
}

func renderPaletteParam(m PaletteModel) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

	filtered := m.filterParamOptions()
	for i, o := range filtered {
		display := core.ProjectDisplayName(o)
		if i == m.paramCursor {
			b.WriteString(selItemStyle.Render(fmt.Sprintf("▸ %s", display)))
		} else {
			b.WriteString(itemStyle.Render(fmt.Sprintf("  %s", display)))
		}
		if i < len(filtered)-1 {
			b.WriteString("\n")
		}
	}
	if len(filtered) == 0 {
		b.WriteString(descStyle.Render("(no matching items)"))
	}
	return b.String()
}
