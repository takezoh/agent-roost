package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/state"
)

// ShortHelp implements help.KeyMap with a curated one-line set of bindings.
func (k KeyMap) ShortHelp() []key.Binding {
	filter := key.NewBinding(key.WithKeys("1", "2", "3", "4", "5"), key.WithHelp("1-5", "filter"))
	return []key.Binding{k.Up, k.Down, k.Enter, k.Toggle, k.New, k.Stop, filter, k.WorkspacePrev}
}

// FullHelp implements help.KeyMap with the complete binding table.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Toggle},
		{k.New, k.NewCmd, k.Stop},
		{k.Filter1, k.Filter2, k.Filter3, k.Filter4, k.Filter5, k.FilterReset},
		{k.WorkspacePrev, k.WorkspaceNext, k.WorkspaceReset},
	}
}

type KeyMap struct {
	Up             key.Binding
	Down           key.Binding
	Enter          key.Binding
	New            key.Binding
	NewCmd         key.Binding
	Stop           key.Binding
	Toggle         key.Binding
	Filter1        key.Binding
	Filter2        key.Binding
	Filter3        key.Binding
	Filter4        key.Binding
	Filter5        key.Binding
	FilterReset    key.Binding
	WorkspacePrev  key.Binding
	WorkspaceNext  key.Binding
	WorkspaceReset key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "switch")),
		New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		NewCmd:      key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "command")),
		Stop:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "stop")),
		Toggle:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "fold")),
		Filter1:        key.NewBinding(key.WithKeys("1")),
		Filter2:        key.NewBinding(key.WithKeys("2")),
		Filter3:        key.NewBinding(key.WithKeys("3")),
		Filter4:        key.NewBinding(key.WithKeys("4")),
		Filter5:        key.NewBinding(key.WithKeys("5")),
		FilterReset:    key.NewBinding(key.WithKeys("0")),
		WorkspacePrev:  key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev workspace")),
		WorkspaceNext:  key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next workspace")),
		WorkspaceReset: key.NewBinding(key.WithKeys("`"), key.WithHelp("`", "reset to default")),
	}
}

// handleWorkspaceKey handles the workspace switcher shortcuts ([ prev, ] next,
// ` reset to All). Returns handled=true when the key matched a binding.
func (m Model) handleWorkspaceKey(msg tea.KeyPressMsg) (Model, bool) {
	switch {
	case key.Matches(msg, m.keys.WorkspacePrev):
		m.selectedWorkspace = prevWorkspace(m.workspaces, m.selectedWorkspace)
	case key.Matches(msg, m.keys.WorkspaceNext):
		m.selectedWorkspace = nextWorkspace(m.workspaces, m.selectedWorkspace)
	case key.Matches(msg, m.keys.WorkspaceReset):
		m.selectedWorkspace = config.DefaultWorkspaceName
	default:
		return m, false
	}
	m.rebuildItems()
	return m, true
}

// handleFilterKey handles the status filter shortcuts (1-5 toggle, 0 reset).
// Returns handled=true when the key matched a filter binding.
func (m Model) handleFilterKey(msg tea.KeyPressMsg) (Model, bool) {
	switch {
	case key.Matches(msg, m.keys.Filter1):
		m.filter.toggle(state.StatusRunning)
	case key.Matches(msg, m.keys.Filter2):
		m.filter.toggle(state.StatusWaiting)
	case key.Matches(msg, m.keys.Filter3):
		m.filter.toggle(state.StatusIdle)
	case key.Matches(msg, m.keys.Filter4):
		m.filter.toggle(state.StatusStopped)
	case key.Matches(msg, m.keys.Filter5):
		m.filter.toggle(state.StatusPending)
	case key.Matches(msg, m.keys.FilterReset):
		m.filter.toggleAll()
	default:
		return m, false
	}
	m.rebuildItems()
	return m, true
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.hovering = false
	if model, handled := m.handleWorkspaceKey(msg); handled {
		return model, nil
	}
	if model, handled := m.handleFilterKey(msg); handled {
		return model, nil
	}
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.ID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.ID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Enter):
		if s := m.cursorSession(); s != nil {
			m.anchored = s.ID
			return m, m.focusCmd(mainPane)
		}
	case key.Matches(msg, m.keys.New):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
			"command": m.cfg.Session.DefaultCommand,
		})
	case key.Matches(msg, m.keys.NewCmd):
		return m, m.launchToolCmd("new-session", map[string]string{
			"project": m.cursorProjectPath(),
		})
	case key.Matches(msg, m.keys.Stop):
		if s := m.cursorSession(); s != nil {
			return m, m.launchToolCmd("stop-session", map[string]string{
				"session_id": s.ID,
			})
		}
	case key.Matches(msg, m.keys.Toggle):
		name := m.cursorProjectName()
		if name != "" {
			m.folded[name] = !m.folded[name]
			m.rebuildItems()
		}
	}
	return m, nil
}
