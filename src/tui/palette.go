package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/tools"
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
	worktreeOn   bool
	projectIsGit bool // cached on entering command param phase; false = hide wt chip

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
	m.worktreeOn = ctx != nil && ctx.Args["worktree"] == "on"
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
