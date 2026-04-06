package tui

import "charm.land/bubbles/v2/key"

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
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "上")),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "下")),
		Enter:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "切替")),
		New:        key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "新規")),
		NewCmd:     key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "コマンド")),
		Stop:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "停止")),
		Toggle:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "折畳")),
	}
}
