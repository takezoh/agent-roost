package tui

import (
	"log/slog"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/tools"
)

var (
	escapeBinding = key.NewBinding(key.WithKeys("esc", "escape"))
	enterBinding  = key.NewBinding(key.WithKeys("enter"))
	upBinding     = key.NewBinding(key.WithKeys("up", "ctrl+p"))
	downBinding   = key.NewBinding(key.WithKeys("down", "ctrl+n"))
	bsBinding     = key.NewBinding(key.WithKeys("backspace"))
	tabBinding    = key.NewBinding(key.WithKeys("tab"))
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
	if m.ctx.Args != nil {
		for k, v := range m.ctx.Args {
			m.paramArgs[k] = v
		}
	}
	m.worktreeOn = m.paramArgs["worktree"] == "on"
	return m.advanceParam()
}

func (m PaletteModel) advanceParam() (tea.Model, tea.Cmd) {
	for m.paramIndex < len(m.selectedTool.Params) {
		p := m.selectedTool.Params[m.paramIndex]
		if _, filled := m.paramArgs[p.Name]; filled {
			m.paramIndex++
			continue
		}
		m.phase = phaseParamSelect
		m.paramOptions = p.Options(m.ctx)
		m.paramCursor = 0
		m.input = ""
		if m.selectedTool != nil && m.selectedTool.Name == "new-session" && p.Name == "command" {
			if m.worktreeOn {
				m.paramArgs["worktree"] = "on"
			} else {
				delete(m.paramArgs, "worktree")
			}
		}
		return m, nil
	}

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
	case key.Matches(msg, tabBinding):
		if m.selectedTool != nil && m.selectedTool.Name == "new-session" &&
			m.paramIndex < len(m.selectedTool.Params) &&
			m.selectedTool.Params[m.paramIndex].Name == "command" {
			m.worktreeOn = !m.worktreeOn
			if m.worktreeOn {
				m.paramArgs["worktree"] = "on"
			} else {
				delete(m.paramArgs, "worktree")
			}
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
