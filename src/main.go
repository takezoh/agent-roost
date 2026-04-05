package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/take/cdeck/config"
	"github.com/take/cdeck/session"
	"github.com/take/cdeck/tmux"
	"github.com/take/cdeck/tui"
)

func main() {
	if os.Getenv("CDECK_TUI") == "1" {
		runSessionList()
		return
	}
	runProcessHandler()
}

func runProcessHandler() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: %v\n", err)
		os.Exit(1)
	}

	sessionName := cfg.Tmux.SessionName
	client := tmux.NewClient(sessionName)

	if client.SessionExists() {
		restoreSession(client, cfg, sessionName)
	} else {
		setupNewSession(client, cfg, sessionName)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go healthMonitor(ctx, client, cfg, sessionName)

	client.Attach()
	cancel()

	if val, _ := client.GetEnv("CDECK_SHUTDOWN"); val == "1" {
		client.KillSession()
	}
}

func setupNewSession(client *tmux.Client, cfg *config.Config, sn string) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if err := client.CreateSession(w, h); err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: create session: %v\n", err)
		os.Exit(1)
	}

	client.SetOption(sn+":0", "remain-on-exit", "on")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)

	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal
	logHeight := 100 - cfg.Tmux.PaneRatioVertical
	if err := client.SplitWindow(sn+":0", true, tuiWidth); err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: split horizontal: %v\n", err)
		os.Exit(1)
	}
	if err := client.SplitWindow(sn+":0.0", false, logHeight); err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: split vertical: %v\n", err)
		os.Exit(1)
	}

	exePath := resolveExe()
	client.SendKeys(sn+":0.2", "CDECK_TUI=1 "+exePath)
	client.SendKeys(sn+":0.0", "echo 'cdeck: prefix+Space to toggle TUI'")

	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)

	client.BindKeyRaw(`unbind-key -a -T prefix`)
	client.BindKeyRaw(`bind-key -T prefix Space if-shell -F "#{==:#{pane_index},2}" "select-pane -t ` + sn + `:0.0" "select-pane -t ` + sn + `:0.2"`)
	client.BindKeyRaw(`bind-key -T prefix d detach-client`)
	client.BindKeyRaw(`bind-key -T prefix q set-environment -t ` + sn + ` CDECK_SHUTDOWN 1 '\;' detach-client`)
	client.SelectPane(sn + ":0.0")
}

func restoreSession(client *tmux.Client, cfg *config.Config, sn string) {
	client.Run("select-window", "-t", sn+":0")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal
	logHeight := 100 - cfg.Tmux.PaneRatioVertical
	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)
	respawnTUIIfDead(client, sn)
	client.SelectPane(sn + ":0.0")
}

func healthMonitor(ctx context.Context, client *tmux.Client, cfg *config.Config, sn string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !client.SessionExists() {
				return
			}
			respawnTUIIfDead(client, sn)
		}
	}
}

func respawnTUIIfDead(client *tmux.Client, sn string) {
	dead, _ := client.Run("display-message", "-t", sn+":0.2", "-p", "#{pane_dead}")
	if dead == "1" {
		client.RespawnPane(sn+":0.2", "CDECK_TUI=1 "+resolveExe())
	}
}

func resolveExe() string {
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}

func runSessionList() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: %v\n", err)
		os.Exit(1)
	}

	client := tmux.NewClient(cfg.Tmux.SessionName)
	manager, err := session.NewManager(client, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: manager: %v\n", err)
		os.Exit(1)
	}

	if err := manager.Reconcile(); err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: reconcile: %v\n", err)
		os.Exit(1)
	}

	monitor := tmux.NewMonitor(client, cfg.Monitor.IdleThresholdSec)
	model := tui.NewModel(manager, monitor, client, cfg)

	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "cdeck: tui: %v\n", err)
		os.Exit(1)
	}
}
