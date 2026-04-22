package driver

import (
	"strings"

	"github.com/takezoh/agent-roost/state"
)

// handleStatusLineClick fires when the user clicks a named region in the tmux
// status bar. Range carries the tmux mouse_status_range value registered via
// #[range=user|<name>] in planStatusLine. A "plan" range with a known plan
// file emits EffPushDriver to open the file in the configured pager.
func (d ClaudeDriver) handleStatusLineClick(cs ClaudeState, e state.DEvStatusLineClick) (ClaudeState, []state.Effect) {
	if e.Range != "plan" || cs.PlanFile == "" {
		return cs, nil
	}
	return cs, []state.Effect{
		state.EffPushDriver{Command: d.pager + " " + shellQuote(cs.PlanFile)},
	}
}

// shellQuote wraps s in single quotes with internal single-quote escaping,
// making it safe to embed in a POSIX shell command string.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
