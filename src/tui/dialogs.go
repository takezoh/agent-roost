package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

type DialogMode int

const (
	DialogNone DialogMode = iota
	DialogProjectSelect
	DialogCommandSelect
	DialogConfirmStop
)

type Dialog struct {
	mode           DialogMode
	items          []string // projects or commands
	cursor         int
	project        string
	command        string
	session        string
	addProjectOnly bool
}

func NewProjectDialog(projects []string, command string) Dialog {
	names := make([]string, len(projects))
	copy(names, projects)
	return Dialog{
		mode:    DialogProjectSelect,
		items:   names,
		command: command,
	}
}

func NewCommandDialog(commands []string, project string) Dialog {
	return Dialog{
		mode:    DialogCommandSelect,
		items:   commands,
		project: project,
	}
}

func NewConfirmDialog(sessionID string) Dialog {
	return Dialog{
		mode:    DialogConfirmStop,
		session: sessionID,
	}
}

func (d *Dialog) Update(msg tea.Msg) (bool, string) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return false, ""
	}

	escBinding := key.NewBinding(key.WithKeys("escape"))
	if key.Matches(km, escBinding) {
		d.mode = DialogNone
		return true, ""
	}

	switch d.mode {
	case DialogProjectSelect, DialogCommandSelect:
		upBinding := key.NewBinding(key.WithKeys("up", "k"))
		downBinding := key.NewBinding(key.WithKeys("down", "j"))
		enterBinding := key.NewBinding(key.WithKeys("enter"))
		switch {
		case key.Matches(km, upBinding) && d.cursor > 0:
			d.cursor--
		case key.Matches(km, downBinding) && d.cursor < len(d.items)-1:
			d.cursor++
		case key.Matches(km, enterBinding) && len(d.items) > 0:
			sel := d.items[d.cursor]
			d.mode = DialogNone
			return true, sel
		}
	case DialogConfirmStop:
		switch km.String() {
		case "y":
			id := d.session
			d.mode = DialogNone
			return true, id
		case "n":
			d.mode = DialogNone
			return true, ""
		}
	}
	return false, ""
}

var dialogBorder = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(1, 2).
	BorderForeground(lipgloss.Color("#7D56F4"))

func (d Dialog) View() string {
	var b strings.Builder

	switch d.mode {
	case DialogProjectSelect:
		b.WriteString("プロジェクト選択\n\n")
		for i, p := range d.items {
			cursor := "  "
			if i == d.cursor {
				cursor = "▸ "
			}
			fmt.Fprintf(&b, "%s%s\n", cursor, filepath.Base(p))
		}
		if len(d.items) == 0 {
			b.WriteString("  (project_roots にプロジェクトなし)")
		}
	case DialogCommandSelect:
		fmt.Fprintf(&b, "コマンド選択: %s\n\n", filepath.Base(d.project))
		for i, cmd := range d.items {
			cursor := "  "
			if i == d.cursor {
				cursor = "▸ "
			}
			fmt.Fprintf(&b, "%s%s\n", cursor, cmd)
		}
	case DialogConfirmStop:
		b.WriteString("停止しますか? [y/n]")
	}

	return dialogBorder.Render(b.String())
}

func (d Dialog) Active() bool {
	return d.mode != DialogNone
}
