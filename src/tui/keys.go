package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Enter      key.Binding
	New        key.Binding
	NewCmd     key.Binding
	Stop       key.Binding
	Toggle     key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "switch")),
		New:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		NewCmd:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "command")),
		Stop:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "stop")),
		Toggle:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "fold")),
	}
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.hovering = false
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
		}
		return m, m.cursorPreviewCmd()
	case key.Matches(msg, m.keys.Enter):
		if s := m.cursorSession(); s != nil {
			m.anchored = s.WindowID
			return m, m.focusCmd("0.0")
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
