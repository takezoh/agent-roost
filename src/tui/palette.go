package tui

import (
	"fmt"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/take/agent-roost/tools"
)

type palettePhase int

const (
	phaseToolSelect palettePhase = iota
	phaseParamSelect
)

type PaletteModel struct {
	registry    *tools.Registry
	ctx         *tools.ToolContext
	initialTool string
	phase       palettePhase
	input       string
	filtered    []tools.Tool
	cursor      int

	// parameter input
	selectedTool *tools.Tool
	paramIndex   int
	paramArgs    map[string]string
	paramOptions []string
	paramCursor  int

	width  int
	height int
	err    error
}

func NewPaletteModel(registry *tools.Registry, ctx *tools.ToolContext, initialTool string) PaletteModel {
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

func (m PaletteModel) startTool(tool *tools.Tool) (tea.Model, tea.Cmd) {
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
	next, err := m.selectedTool.Run(m.ctx, m.paramArgs)
	if err != nil {
		slog.Error("tool execution failed", "tool", m.selectedTool.Name, "args", m.paramArgs, "err", err)
		return m, tea.Quit
	}
	slog.Info("tool executed", "tool", m.selectedTool.Name, "args", m.paramArgs)
	if next != nil && next.Name != "" {
		if t := m.registry.Get(next.Name); t != nil {
			m.selectedTool = t
			m.paramIndex = 0
			m.paramArgs = make(map[string]string, len(next.Args))
			for k, v := range next.Args {
				m.paramArgs[k] = v
			}
			m.input = ""
			m.paramCursor = 0
			return m.advanceParam()
		}
		slog.Error("chained tool not found", "tool", next.Name)
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
		p := m.selectedTool.Params[m.paramIndex]
		var value string
		if len(m.paramOptions) == 0 {
			// Free-form text input mode: triggered when a Param's Options
			// returns nil/empty. Used by tools like create-project (`name`)
			// and stop-session (`session_id`). Note: it also kicks in as a
			// fallback for tools like new-session when their Options happens
			// to be empty (e.g. no projects configured) — by design.
			value = strings.TrimSpace(m.input)
			if value == "" {
				return m, nil
			}
		} else {
			filtered := m.filterParamOptions()
			if len(filtered) == 0 || m.paramCursor >= len(filtered) {
				return m, nil
			}
			value = filtered[m.paramCursor]
		}
		m.paramArgs[p.Name] = value
		m.paramIndex++
		return m.advanceParam()
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
			strings.Contains(strings.ToLower(tools.ProjectDisplayName(o)), q) {
			matched = append(matched, o)
		}
	}
	return matched
}

func (m *PaletteModel) refilter() {
	m.filtered = m.registry.Match(m.input)
	m.cursor = 0
}

// visibleWindow returns the [start, end) slice of items to display so that
// cursor stays visible within a viewport of maxVisible rows.
func visibleWindow(cursor, total, maxVisible int) (start, end int) {
	if total <= maxVisible || maxVisible <= 0 {
		return 0, total
	}
	start = cursor - maxVisible/2
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}

func (m PaletteModel) View() tea.View {
	outerWidth := m.width
	if outerWidth <= 0 || outerWidth > 80 {
		outerWidth = 60
	}

	innerWidth := outerWidth - 4 // border(2) + padding(2)

	var body string
	var title, badge string

	switch m.phase {
	case phaseToolSelect:
		title = "PALETTE"
		badge = fmt.Sprintf("%d tools", len(m.filtered))
		body = renderPaletteTool(m, innerWidth)

	case phaseParamSelect:
		p := m.selectedTool.Params[m.paramIndex]
		title = m.selectedTool.Name
		badge = p.Name
		body = renderPaletteParam(m, innerWidth)
	}

	return tea.NewView(Panel(title, badge, body, outerWidth))
}

func renderPaletteTool(m PaletteModel, innerWidth int) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

	total := len(m.filtered)
	// prompt(1) + blank(1) + border(PanelChromeRows)
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
		if i == m.cursor {
			b.WriteString(selItemStyle.Width(innerWidth).MaxHeight(1).Render(fmt.Sprintf("▸ %s", t.Name) + desc))
		} else {
			b.WriteString(itemStyle.Width(innerWidth).MaxHeight(1).Render(fmt.Sprintf("  %s", t.Name) + desc))
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

func renderPaletteParam(m PaletteModel, innerWidth int) string {
	var b strings.Builder
	b.WriteString(promptStyle.Render("> "))
	b.WriteString(inputStyle.Render(m.input))
	b.WriteString("█\n\n")

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
		if i == m.paramCursor {
			b.WriteString(selItemStyle.Width(innerWidth).MaxHeight(1).Render(fmt.Sprintf("▸ %s", display)))
		} else {
			b.WriteString(itemStyle.Width(innerWidth).MaxHeight(1).Render(fmt.Sprintf("  %s", display)))
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
