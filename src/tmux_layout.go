package main

import (
	"fmt"

	"log/slog"

	"golang.org/x/term"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/lib/tmux"
	"github.com/takezoh/agent-roost/uiproc"
)

// Window 0 pane layout (4 panes):
//
//	0.0 HEADER  (1 row, left column only — frame tab bar)
//	0.1 MAIN    (left column, middle — active agent pane)
//	0.2 LOG     (left column, bottom — log viewer)
//	0.3 SESSIONS (right column, full height — session list)
const paneLabel = `#{?#{==:#{window_index},0},` +
	`#{?#{==:#{pane_index},0},[HEADER],#{?#{==:#{pane_index},1},[MAIN],#{?#{==:#{pane_index},2},[LOG],[SESSIONS]}}},` +
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
	logHeight := 100 - cfg.Tmux.PaneRatioVertical

	// Step 1: Split sessions sidebar to the right.
	// After: 0.0=left-area, 0.1=SESSIONS(right).
	if err := client.SplitWindow(sn+":0", true, tuiWidth); err != nil {
		return fmt.Errorf("split sessions: %w", err)
	}
	// Step 2: Split a 1-row header above the left area only.
	// After (DFS re-index): 0.0=HEADER, 0.1=left-area, 0.2=SESSIONS.
	if err := client.SplitWindowRows(sn+":0.0", true, 1); err != nil {
		return fmt.Errorf("split header: %w", err)
	}
	// Step 3: Split the left area vertically for the log pane.
	// After (DFS re-index): 0.0=HEADER, 0.1=MAIN, 0.2=LOG, 0.3=SESSIONS.
	if err := client.SplitWindow(sn+":0.1", false, logHeight); err != nil {
		return fmt.Errorf("split log: %w", err)
	}

	exePath := resolveExe()
	envPrefix := "ROOST_SESSION_ID=" + sn + " "
	_ = client.SendKeys(sn+":0.3", envPrefix+uiproc.Sessions().Command(exePath))
	_ = client.SendKeys(sn+":0.2", envPrefix+uiproc.Log().Command(exePath))
	_ = client.SendKeys(sn+":0.1", envPrefix+uiproc.Main().Command(exePath))
	_ = client.SendKeys(sn+":0.0", envPrefix+uiproc.Header().Command(exePath))

	paneMain, _ := client.DisplayMessage(sn+":0.1", "#{pane_id}")
	_ = client.SetEnv("ROOST_SESSION__main", paneMain)
	paneHeader, _ := client.DisplayMessage(sn+":0.0", "#{pane_id}")
	_ = client.SetEnv("ROOST_SESSION__header", paneHeader)

	_ = client.ResizePane(sn+":0.3", tuiWidth, 0)
	_ = client.ResizePane(sn+":0.2", 0, logHeight)
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
	logHeight := 100 - cfg.Tmux.PaneRatioVertical

	_ = client.ResizePane(sn+":0.3", tuiWidth, 0)
	_ = client.ResizePane(sn+":0.2", 0, logHeight)
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
		k + prefix + " z" + d + " zoom" + sep +
		k + prefix + " p" + d + " palette" + sep +
		k + prefix + " C-p" + d + " push" + sep +
		k + prefix + " d" + d + " detach" + sep +
		k + prefix + " q" + d + " quit"

	log := k + "g" + d + " top" + sep +
		k + "G" + d + " bottom" + sep +
		k + "↑/↓" + d + " scroll"

	sessions := k + "n" + d + " new" + sep +
		k + "N" + d + " cmd" + sep +
		k + "Enter" + d + " switch" + sep +
		k + "d" + d + " stop" + sep +
		k + "Tab" + d + " fold" + sep +
		k + "1-5/0" + d + " filter"

	other := k + prefix + " Space" + d + " toggle"

	return "#{?#{==:#{window_index},0}," +
		"#{?#{==:#{pane_index},1}," + main + "," +
		"#{?#{==:#{pane_index},2}," + log + "," +
		"#{?#{==:#{pane_index},3}," + sessions + "," + other + "}}}," +
		other + "}"
}

func setupKeyBindings(client *tmux.Client, sn string) {
	exePath := resolveExe()
	_ = client.UnbindAllKeys("prefix")
	_ = client.BindKey("prefix", "Space",
		"if-shell", "-F", `#{==:#{pane_index},3}`,
		"select-pane -t "+sn+":0.1",
		"select-pane -t "+sn+":0.3")
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

func respawnHeaderPane(client *tmux.Client, sn string) {
	_ = client.RespawnPane(sn+":0.0", uiproc.Header().Command(resolveExe()))
}

func respawnSessionsPane(client *tmux.Client, sn string) {
	_ = client.RespawnPane(sn+":0.3", uiproc.Sessions().Command(resolveExe()))
}

func respawnLogPane(client *tmux.Client, sn string) {
	_ = client.RespawnPane(sn+":0.2", uiproc.Log().Command(resolveExe()))
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
