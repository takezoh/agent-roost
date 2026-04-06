package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/logger"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/tmux"
	"github.com/take/agent-roost/tui"
)

func main() {
	logger.Init()
	defer logger.Close()

	if len(os.Args) > 1 && os.Args[1] == "--tui" && len(os.Args) > 2 {
		switch os.Args[2] {
		case "sessions":
			runSessionList()
		case "palette":
			runPalette(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "roost: unknown tui: %s\n", os.Args[2])
			os.Exit(1)
		}
		return
	}
	runProcessHandler()
}

func runProcessHandler() {
	cfg := loadConfig()
	sessionName := cfg.Tmux.SessionName
	client := tmux.NewClient(sessionName)

	if client.SessionExists() {
		restoreSession(client, cfg, sessionName)
	} else {
		setupNewSession(client, cfg, sessionName)
	}

	mgr := session.NewManager(client, cfg.ResolveDataDir())
	mgr.Refresh()

	monitor := tmux.NewMonitor(client, cfg.Monitor.IdleThresholdSec)
	svc := core.NewService(mgr, monitor, client, sessionName)

	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	srv := core.NewServer(svc, client, sockPath)
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: server: %v\n", err)
		os.Exit(1)
	}
	go srv.StartMonitor(cfg.Monitor.PollIntervalMs)
	defer srv.Stop()

	respawnTUI(client, sessionName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go healthMonitor(ctx, client, cfg, sessionName)

	client.Attach()

	if srv.ShutdownRequested() {
		client.KillSession()
		mgr.Clear()
	}
}

func setupNewSession(client *tmux.Client, cfg *config.Config, sn string) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if err := client.CreateSession(w, h); err != nil {
		fmt.Fprintf(os.Stderr, "roost: create session: %v\n", err)
		os.Exit(1)
	}

	client.SetOption(sn+":0", "remain-on-exit", "on")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)

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
	client.SendKeys(sn+":0.2", exePath+" --tui sessions")
	client.SendKeys(sn+":0.0", "echo 'roost: prefix+Space to toggle TUI'")

	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)

	setupKeyBindings(client, sn)
	client.SelectPane(sn + ":0.0")
}

func restoreSession(client *tmux.Client, cfg *config.Config, sn string) {
	client.Run("select-window", "-t", sn+":0")
	client.SetOption(sn, "prefix", cfg.Tmux.Prefix)
	tuiWidth := 100 - cfg.Tmux.PaneRatioHorizontal
	logHeight := 100 - cfg.Tmux.PaneRatioVertical
	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)
	setupKeyBindings(client, sn)
	client.SelectPane(sn + ":0.0")
}

func setupKeyBindings(client *tmux.Client, sn string) {
	exePath := resolveExe()
	client.BindKeyRaw(`unbind-key -a -T prefix`)
	client.BindKeyRaw(`bind-key -T prefix Space if-shell -F "#{==:#{pane_index},2}" "select-pane -t ` + sn + `:0.0" "select-pane -t ` + sn + `:0.2"`)
	client.BindKeyRaw(`bind-key -T prefix d detach-client`)
	client.BindKeyRaw(`bind-key -T prefix q display-popup -E -w 40% -h 20% "echo 'Shutting down...' && ` + exePath + ` --tui palette --tool=shutdown"`)
	client.BindKeyRaw(`bind-key -T prefix p display-popup -E -w 60% -h 50% "` + exePath + ` --tui palette"`)
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

func respawnTUI(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.2", resolveExe()+" --tui sessions")
}

func respawnTUIIfDead(client *tmux.Client, sn string) {
	dead, _ := client.Run("display-message", "-t", sn+":0.2", "-p", "#{pane_dead}")
	if dead == "1" {
		respawnTUI(client, sn)
	}
}

func runSessionList() {
	cfg := loadConfig()
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := core.Dial(sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: connect: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
	client.StartListening()
	client.Subscribe()

	model := tui.NewModel(client, cfg)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: tui: %v\n", err)
		os.Exit(1)
	}
}

func runPalette(args []string) {
	slog.Info("palette start", "args", args)
	cfg := loadConfig()
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	slog.Info("palette dial", "sock", sockPath)

	client, err := core.Dial(sockPath)
	if err != nil {
		slog.Error("palette connect failed", "err", err)
		fmt.Fprintf(os.Stderr, "roost: connect: %v\n", err)
		os.Exit(1)
	}
	slog.Info("palette connected")
	defer client.Close()
	client.StartListening()

	var toolName string
	prefill := make(map[string]string)
	for _, a := range args {
		if strings.HasPrefix(a, "--tool=") {
			toolName = strings.TrimPrefix(a, "--tool=")
		} else if strings.HasPrefix(a, "--arg=") {
			kv := strings.TrimPrefix(a, "--arg=")
			if parts := strings.SplitN(kv, "=", 2); len(parts) == 2 {
				prefill[parts[0]] = parts[1]
			}
		}
	}

	registry := tui.DefaultRegistry()
	ctx := &tui.ToolContext{
		Client: client,
		Config: tui.ToolConfig{
			DefaultCommand: cfg.Session.DefaultCommand,
			Commands:       cfg.Session.Commands,
			Projects:       cfg.ListProjects(),
		},
		Args: prefill,
	}

	model := tui.NewPaletteModel(registry, ctx, toolName)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: palette: %v\n", err)
		os.Exit(1)
	}
}

func loadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func resolveExe() string {
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}
