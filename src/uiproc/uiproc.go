// Package uiproc defines the four UI subprocess identities (main / log /
// sessions / palette) and produces the shell command to spawn them.
//
// No external imports: keeping this package stdlib-only allows state/ to
// reference it without violating Driver/Connector isolation.
package uiproc

import (
	"sort"
	"strings"
)

// UIProcess identifies a roost UI subprocess and its launch parameters.
// ExtraArgs are pre-shell-quoted because every spawn path (tmux
// send-keys, respawn-pane, display-popup) feeds the string to a shell.
type UIProcess struct {
	Name       string   // "main" | "log" | "sessions" | "palette"
	PaneSuffix string   // ":0.0" | ":0.1" | ":0.2" | "" (popup-only)
	Subcommand string   // token after --tui flag
	ExtraArgs  []string // pre-encoded CLI args (values shell-quoted)
}

// Main returns the UIProcess for the main TUI (pane 0.0).
func Main() UIProcess {
	return UIProcess{Name: "main", PaneSuffix: ":0.0", Subcommand: "main"}
}

// Log returns the UIProcess for the log viewer (pane 0.1).
func Log() UIProcess {
	return UIProcess{Name: "log", PaneSuffix: ":0.1", Subcommand: "log"}
}

// Sessions returns the UIProcess for the session list (pane 0.2).
func Sessions() UIProcess {
	return UIProcess{Name: "sessions", PaneSuffix: ":0.2", Subcommand: "sessions"}
}

// Palette returns the UIProcess for the command palette (popup).
// tool is the palette tool name; pass "" to open the default palette.
// args is an optional map of prefill key=value pairs; empty values are skipped.
// Keys are sorted for deterministic output.
func Palette(tool string, args map[string]string) UIProcess {
	var extra []string
	if tool != "" {
		extra = append(extra, "--tool="+shellQuote(tool))
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if v := args[k]; v != "" {
			extra = append(extra, "--arg="+shellQuote(k+"="+v))
		}
	}
	return UIProcess{Name: "palette", Subcommand: "palette", ExtraArgs: extra}
}

// RespawnTarget returns the UIProcess that should be respawned when the
// given pane (in {sessionName}:0.N placeholder form) dies. Only handles
// control panes 0.1 and 0.2; pane 0.0 requires an active-session check
// that callers handle directly with Main().
func RespawnTarget(pane string) (UIProcess, bool) {
	switch {
	case strings.HasSuffix(pane, ":0.1"):
		return Log(), true
	case strings.HasSuffix(pane, ":0.2"):
		return Sessions(), true
	}
	return UIProcess{}, false
}

// Command returns the full shell command to spawn this process.
// roostExe is the path to the roost executable (will be shell-quoted).
func (p UIProcess) Command(roostExe string) string {
	cmd := shellQuote(roostExe) + " --tui " + p.Subcommand
	for _, a := range p.ExtraArgs {
		cmd += " " + a
	}
	return cmd
}

// shellQuote wraps s in single quotes and escapes any embedded single
// quotes, preventing shell injection.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
