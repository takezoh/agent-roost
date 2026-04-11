package main

import (
	"fmt"
	"os"

	"log/slog"

	"golang.org/x/term"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/lib/tmux"
)

const paneLabel = `#{?#{==:#{window_index},0},` +
	`#{?#{==:#{pane_index},0},[MAIN],#{?#{==:#{pane_index},1},[LOG],[SESSIONS]}},` +
	`[#{window_name}]}`

func setupNewSession(client *tmux.Client, cfg *config.Config, sn string) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	slog.Info("setup new session", "width", w, "height", h)
	if err := client.CreateSession(w, h); err != nil {
		fmt.Fprintf(os.Stderr, "roost: create session: %v\n", err)
		os.Exit(1)
	}

	client.SetOption(sn+":0", "remain-on-exit", "on")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	client.SetOption(sn, "mouse", "on")

	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal
	logHeight := 100 - cfg.Tmux.PaneRatioVertical
	if err := client.SplitWindow(sn+":0", true, tuiWidth); err != nil {
		fmt.Fprintf(os.Stderr, "roost: split horizontal: %v\n", err)
		os.Exit(1)
	}
	if err := client.SplitWindow(sn+":0.0", false, logHeight); err != nil {
		fmt.Fprintf(os.Stderr, "roost: split vertical: %v\n", err)
		os.Exit(1)
	}

	exePath := resolveExe()
	envPrefix := "ROOST_SESSION_ID=" + sn + " "
	client.SendKeys(sn+":0.2", envPrefix+exePath+" --tui sessions")
	client.SendKeys(sn+":0.1", envPrefix+exePath+" --tui log")
	client.SendKeys(sn+":0.0", envPrefix+exePath+" --tui main")

	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)

	setupKeyBindings(client, sn)
	setupStatusBar(client, sn, cfg.Tmux.Prefix)
	client.SelectPane(sn + ":0.0")
}

func restoreSession(client *tmux.Client, cfg *config.Config, sn string) {
	slog.Info("restore session")
	client.Run("select-window", "-t", sn+":0")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	client.SetOption(sn, "mouse", "on")
	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal
	logHeight := 100 - cfg.Tmux.PaneRatioVertical
	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)
	setupKeyBindings(client, sn)
	setupStatusBar(client, sn, cfg.Tmux.Prefix)
	client.SelectPane(sn + ":0.0")
}

func setupStatusBar(client *tmux.Client, sn string, prefix string) {
	client.SetOption(sn, "status-left", " ")
	client.SetOption(sn, "status-left-length", "120")
	client.SetOption(sn, "status-style", "bg=#1d2021,fg=#ebdbb2")
	client.Run("set-option", "-t", sn, "status-format[0]",
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
		"#{?#{==:#{pane_index},0}," + main + "," +
		"#{?#{==:#{pane_index},1}," + log + "," + sessions + "}}," +
		other + "}"
}

func setupKeyBindings(client *tmux.Client, sn string) {
	exePath := resolveExe()
	client.UnbindAllKeys("prefix")
	client.BindKey("prefix", "Space",
		"if-shell", "-F", `#{==:#{pane_index},2}`,
		"select-pane -t "+sn+":0.0",
		"select-pane -t "+sn+":0.2")
	client.BindKey("prefix", "Escape", "run-shell", exePath+" event preview-project")
	client.BindKey("prefix", "z", "resize-pane", "-Z", "-t", sn+":0.0")
	client.BindKey("prefix", "d", "detach-client")
	client.BindKey("prefix", "q",
		"display-popup", "-E", "-w", "40%", "-h", "20%",
		"echo 'Shutting down...' && "+exePath+" --tui palette --tool=shutdown")
	client.BindKey("prefix", "p",
		"display-popup", "-E", "-w", "60%", "-h", "50%",
		exePath+" --tui palette")
}

func respawnMainPane(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.0", resolveExe()+" --tui main")
}

func respawnSessionsPane(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.2", resolveExe()+" --tui sessions")
}

func respawnLogPane(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.1", resolveExe()+" --tui log")
}
