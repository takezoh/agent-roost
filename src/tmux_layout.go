package main

import (
	"fmt"

	"log/slog"

	"golang.org/x/term"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/lib/tmux"
	"github.com/takezoh/agent-roost/uiproc"
)

// Window 0 pane layout (3 panes):
//
//	0.0 HEADER   (1 row, left column only — frame tab bar)
//	0.1 MAIN     (left column — active agent / main TUI / log TUI)
//	0.2 SESSIONS (right column, full height — session list)
const paneLabel = `#{?#{==:#{window_index},0},` +
	`#{?#{==:#{pane_index},0},[HEADER],#{?#{==:#{pane_index},1},[MAIN],[SESSIONS]}},` +
	`[#{window_name}]}`

func setupNewSession(client *tmux.Client, cfg *config.Config, sn string) error {
	w, h, _ := term.GetSize(1)
	slog.Info("setup new session", "width", w, "height", h)
	if err := client.CreateSession(w, h); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	_ = client.SetOption(sn+":0", "remain-on-exit", "on")
	_ = client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	_ = client.SetOption(sn, "mouse", "on")
	enableHyperlinkForward(client)

	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal

	// Step 1: Split sessions sidebar to the right.
	// After: 0.0=left-area, 0.1=SESSIONS(right).
	if err := client.SplitWindow(sn+":0", true, tuiWidth); err != nil {
		return fmt.Errorf("split sessions: %w", err)
	}
	// Step 2: Split a 1-row header above the left area only.
	// After (DFS re-index): 0.0=HEADER, 0.1=MAIN, 0.2=SESSIONS.
	if err := client.SplitWindowRows(sn+":0.0", true, 1); err != nil {
		return fmt.Errorf("split header: %w", err)
	}

	exePath := resolveExe()
	envPrefix := "ROOST_SESSION_ID=" + sn + " "
	_ = client.SendKeys(sn+":0.2", envPrefix+uiproc.Sessions().Command(exePath))
	_ = client.SendKeys(sn+":0.1", envPrefix+uiproc.Main().Command(exePath))
	_ = client.SendKeys(sn+":0.0", envPrefix+uiproc.Header().Command(exePath))

	paneMain, _ := client.DisplayMessage(sn+":0.1", "#{pane_id}")
	_ = client.SetEnv("ROOST_FRAME__main", paneMain)

	// Create the __hidden__ window to house the log TUI as a persistent process.
	// The window stays detached so it never steals focus. remain-on-exit keeps
	// the pane alive after the log TUI exits (for crash recovery via respawn).
	hiddenPaneID, err := client.Run(
		"new-window", "-d", "-n", "__hidden__", "-t", sn+":",
		"-P", "-F", "#{pane_id}",
		envPrefix+uiproc.Log().Command(exePath),
	)
	if err != nil {
		return fmt.Errorf("hidden window: %w", err)
	}
	_ = client.SetOption(sn+":__hidden__", "remain-on-exit", "on")
	_ = client.SetEnv("ROOST_HIDDEN_PANE", hiddenPaneID)

	_ = client.ResizePane(sn+":0.2", tuiWidth, 0)
	setupKeyBindings(client, sn)

	setupStatusBar(client, sn, cfg.Tmux.Prefix)
	_ = client.SelectPane(sn + ":0.1")
	return nil
}

func restoreSession(client *tmux.Client, cfg *config.Config, sn string) {
	slog.Info("restore session")
	_, _ = client.Run("select-window", "-t", sn+":0")
	_ = client.SetOption(sn+":0", "remain-on-exit", "on")
	_ = client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	_ = client.SetOption(sn, "mouse", "on")
	enableHyperlinkForward(client)

	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal

	_ = client.ResizePane(sn+":0.2", tuiWidth, 0)
	setupKeyBindings(client, sn)
	setupStatusBar(client, sn, cfg.Tmux.Prefix)
	_ = client.SelectPane(sn + ":0.1")
}

func setupStatusBar(client *tmux.Client, sn string, prefix string) {
	_ = client.SetOption(sn, "status-left", " ")
	_ = client.SetOption(sn, "status-left-length", "120")
	_ = client.SetOption(sn, "status-style", "bg=#1d2021,fg=#ebdbb2")
	_, _ = client.Run("set-option", "-t", sn, "status-format[0]",
		" "+paneLabel+"#{status-left}#[align=right]"+paneHints(prefix)+" ")
}

// paneHints builds a tmux conditional format string that shows different
// keybinding hints depending on which pane is focused.
func paneHints(prefix string) string {
	k := "#[bold]#[fg=#ebdbb2]"
	d := "#[nobold]#[fg=#626262]"
	sep := d + " · "

	main := k + prefix + " Space" + d + " toggle" + sep +
		k + prefix + " m" + d + " main" + sep +
		k + prefix + " l" + d + " log" + sep +
		k + prefix + " z" + d + " zoom" + sep +
		k + prefix + " p" + d + " palette" + sep +
		k + prefix + " C-p" + d + " push" + sep +
		k + prefix + " d" + d + " detach" + sep +
		k + prefix + " q" + d + " quit"

	sessions := k + "n" + d + " new" + sep +
		k + "N" + d + " cmd" + sep +
		k + "Enter" + d + " switch" + sep +
		k + "d" + d + " stop" + sep +
		k + "Tab" + d + " fold" + sep +
		k + "1-5/0" + d + " filter"

	other := k + prefix + " Space" + d + " toggle"

	return "#{?#{==:#{window_index},0}," +
		"#{?#{==:#{pane_index},1}," + main + "," +
		"#{?#{==:#{pane_index},2}," + sessions + "," + other + "}}," +
		other + "}"
}

func setupKeyBindings(client *tmux.Client, sn string) {
	exePath := resolveExe()
	_ = client.UnbindAllKeys("prefix")
	_ = client.BindKey("prefix", "Space",
		"if-shell", "-F", `#{==:#{pane_index},2}`,
		"select-pane -t "+sn+":0.1",
		"select-pane -t "+sn+":0.2")
	_ = client.BindKey("prefix", "l", "run-shell", exePath+" activate-occupant log")
	_ = client.BindKey("prefix", "m", "run-shell", exePath+" activate-occupant main")
	_ = client.BindKey("prefix", "Escape", "run-shell", exePath+" event preview-project")
	_ = client.BindKey("prefix", "z", "resize-pane", "-Z", "-t", sn+":0.1")
	_ = client.BindKey("prefix", "d", "detach-client")
	_ = client.BindKey("prefix", "q",
		"display-popup", "-E", "-w", "40%", "-h", "20%",
		"echo 'Shutting down...' && "+uiproc.Palette("shutdown", nil).Command(exePath))
	_ = client.BindKey("prefix", "p",
		"display-popup", "-E", "-w", "60%", "-h", "50%",
		uiproc.Palette("", nil).Command(exePath))
	_ = client.BindKey("prefix", "C-p",
		"display-popup", "-E", "-w", "60%", "-h", "50%",
		uiproc.Palette("push-driver", nil).Command(exePath))
	// Disable right-click context menus (Horizontal/Vertical Split etc.)
	// without wiping the entire root table, which would break C-b prefix
	// forwarding and mouse drag selection.
	for _, key := range []string{
		"MouseDown3Pane",
		"M-MouseDown3Pane",
		"MouseDown3Status",
		"MouseDown3StatusDefault",
		"MouseDown3StatusLeft",
		"MouseDown3StatusRight",
	} {
		_ = client.UnbindKey("root", key)
	}
}

// ensureHiddenWindow verifies that the __hidden__ window exists on warm
// restart and recreates it with the log TUI if it has been lost. The new
// pane id is stored in the ROOST_HIDDEN_PANE session env var so that the
// next LoadSessionPanes call picks it up.
func ensureHiddenWindow(client *tmux.Client, sn string) {
	windows, err := client.ListWindows()
	if err != nil {
		slog.Warn("restore: could not list windows for hidden-window check", "err", err)
		return
	}
	for _, w := range windows {
		if w.Name == "__hidden__" {
			slog.Info("restore: hidden window present")
			return
		}
	}
	slog.Info("restore: hidden window missing, recreating")
	exePath := resolveExe()
	envPrefix := "ROOST_SESSION_ID=" + sn + " "
	hiddenPaneID, err := client.Run(
		"new-window", "-d", "-n", "__hidden__", "-t", sn+":",
		"-P", "-F", "#{pane_id}",
		envPrefix+uiproc.Log().Command(exePath),
	)
	if err != nil {
		slog.Warn("restore: hidden window recreation failed", "err", err)
		return
	}
	_ = client.SetOption(sn+":__hidden__", "remain-on-exit", "on")
	_ = client.SetEnv("ROOST_HIDDEN_PANE", hiddenPaneID)
	slog.Info("restore: hidden window recreated", "pane", hiddenPaneID)
}

func respawnHeaderPane(client *tmux.Client, sn string) {
	_ = client.RespawnPane(sn+":0.0", uiproc.Header().Command(resolveExe()))
}

func respawnSessionsPane(client *tmux.Client, sn string) {
	_ = client.RespawnPane(sn+":0.2", uiproc.Sessions().Command(resolveExe()))
}

// enableHyperlinkForward appends "hyperlinks" to the server-wide
// terminal-features array so tmux re-emits OSC 8 sequences to the
// outer terminal (WezTerm, Ghostty, Windows Terminal, ...). Without
// this flag tmux 3.4+ consumes OSC 8 as a cell attribute and never
// forwards it. The call is best-effort: older tmux that does not
// recognise the option still continues to start.
func enableHyperlinkForward(client *tmux.Client) {
	if _, err := client.Run("set-option", "-as", "terminal-features", ",*:hyperlinks"); err != nil {
		slog.Warn("tmux terminal-features hyperlinks not applied", "err", err)
	}
}
