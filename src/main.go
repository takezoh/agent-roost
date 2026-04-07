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
	"github.com/take/agent-roost/lib"
	_ "github.com/take/agent-roost/lib/claude"
	"github.com/take/agent-roost/logger"
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
	"github.com/take/agent-roost/tui"
)

func main() {
	logger.Init()
	defer logger.Close()

	if lib.Dispatch(os.Args[1:]) {
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help") {
		printUsage()
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "--tui" && len(os.Args) > 2 {
		switch os.Args[2] {
		case "main":
			runMainTUI()
		case "sessions":
			runSessionList()
		case "log":
			runLogViewer()
		case "palette":
			runPalette(os.Args[3:])
		default:
			fmt.Fprintf(os.Stderr, "roost: unknown tui: %s\n", os.Args[2])
			os.Exit(1)
		}
		return
	}
	runCoordinator()
}

func runCoordinator() {
	cfg := loadConfig()
	sessionName := cfg.Tmux.SessionName
	slog.Info("starting coordinator", "session", sessionName)
	client := tmux.NewClient(sessionName)

	if client.SessionExists() {
		slog.Info("session exists, restoring")
		restoreSession(client, cfg, sessionName)
	} else {
		slog.Info("creating new session")
		setupNewSession(client, cfg, sessionName)
	}

	dataDir := cfg.ResolveDataDir()
	os.MkdirAll(dataDir, 0o755)

	mgr := session.NewManager(client, dataDir)
	mgr.Refresh()
	slog.Info("sessions loaded", "count", len(mgr.All()))

	activeWID := restoreActiveWindowID(client, mgr)

	drivers := driver.DefaultRegistry()
	agentStore := driver.NewAgentStore()
	monitor := tmux.NewMonitor(client, cfg.Monitor.IdleThresholdSec, drivers)
	eventLogDir := filepath.Join(dataDir, "events")
	svc := core.NewService(mgr, agentStore, drivers, monitor, client, sessionName, eventLogDir, activeWID)
	svc.SetSyncActive(func(wid string) {
		if wid != "" {
			client.SetEnv("ROOST_ACTIVE_WINDOW", wid)
		} else {
			client.Run("set-environment", "-t", sessionName, "-u", "ROOST_ACTIVE_WINDOW")
		}
	})
	svc.SetSyncStatus(func(line string) {
		left := " " + paneLabel
		if line != "" {
			left += " " + line
		}
		left += " "
		client.SetOption(sessionName, "status-left", left)
	})

	sockPath := filepath.Join(dataDir, "roost.sock")
	srv := core.NewServer(svc, client, sockPath)
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: server: %v\n", err)
		os.Exit(1)
	}
	slog.Info("server started", "sock", sockPath)
	go srv.StartMonitor(cfg.Monitor.PollIntervalMs)
	defer srv.Stop()

	respawnSessionsPane(client, sessionName)
	respawnLogPane(client, sessionName)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go healthMonitor(ctx, client, cfg, sessionName)

	slog.Info("attaching to tmux session")
	client.Attach()

	if srv.ShutdownRequested() {
		slog.Info("shutdown requested, cleaning up")
		client.KillSession()
		mgr.Clear()
	} else {
		slog.Info("detached, session kept alive")
	}
}

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
	client.SendKeys(sn+":0.2", exePath+" --tui sessions")
	client.SendKeys(sn+":0.1", exePath+" --tui log")
	client.SendKeys(sn+":0.0", exePath+" --tui main")

	client.ResizePane(sn+":0.2", tuiWidth, 0)
	client.ResizePane(sn+":0.1", 0, logHeight)

	setupKeyBindings(client, sn)
	setupStatusBar(client, sn)
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
	setupStatusBar(client, sn)
	client.SelectPane(sn + ":0.0")
}

const paneLabel = `#{?#{==:#{window_index},0},` +
	`#{?#{==:#{pane_index},0},[MAIN],#{?#{==:#{pane_index},1},[LOG],[SESSIONS]}},` +
	`[#{window_name}]}`

func setupStatusBar(client *tmux.Client, sn string) {
	client.SetOption(sn, "status-left", " "+paneLabel+" ")
	client.SetOption(sn, "status-left-length", "120")
	client.SetOption(sn, "status-right", "")
	client.SetOption(sn, "status-style", "bg=#1d2021,fg=#ebdbb2")
	client.SetOption(sn, "window-status-format", "")
	client.SetOption(sn, "window-status-current-format", "")
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
			respawnSessionsPaneIfDead(client, sn)
			respawnLogPaneIfDead(client, sn)
		}
	}
}

func respawnSessionsPane(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.2", resolveExe()+" --tui sessions")
}

func respawnLogPane(client *tmux.Client, sn string) {
	client.RespawnPane(sn+":0.1", resolveExe()+" --tui log")
}

func respawnLogPaneIfDead(client *tmux.Client, sn string) {
	dead, _ := client.Run("display-message", "-t", sn+":0.1", "-p", "#{pane_dead}")
	if dead == "1" {
		slog.Info("respawning dead pane", "pane", sn+":0.1")
		respawnLogPane(client, sn)
	}
}

func respawnSessionsPaneIfDead(client *tmux.Client, sn string) {
	dead, _ := client.Run("display-message", "-t", sn+":0.2", "-p", "#{pane_dead}")
	if dead == "1" {
		slog.Info("respawning dead pane", "pane", sn+":0.2")
		respawnSessionsPane(client, sn)
	}
}

func runMainTUI() {
	cfg := loadConfig()
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := core.Dial(sockPath)
	if err != nil {
		model := tui.NewMainModel(nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "roost: main: %v\n", err)
			os.Exit(1)
		}
		return
	}
	defer client.Close()
	client.StartListening()
	client.Subscribe()

	model := tui.NewMainModel(client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: main: %v\n", err)
		os.Exit(1)
	}
}

func runLogViewer() {
	cfg := loadConfig()
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := core.Dial(sockPath)
	if err != nil {
		model := tui.NewLogModel(logger.LogFilePath(), nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "roost: log: %v\n", err)
			os.Exit(1)
		}
		return
	}
	defer client.Close()
	client.StartListening()
	client.Subscribe()

	model := tui.NewLogModel(logger.LogFilePath(), client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: log: %v\n", err)
		os.Exit(1)
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

	tools := core.DefaultToolRegistry()
	ctx := &core.ToolContext{
		Client: client,
		Config: core.ToolConfig{
			DefaultCommand: cfg.Session.DefaultCommand,
			Commands:       cfg.Session.Commands,
			Projects:       cfg.ListProjects(),
		},
		Args: prefill,
	}

	model := tui.NewPaletteModel(tools, ctx, toolName)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: palette: %v\n", err)
		os.Exit(1)
	}
}

func restoreActiveWindowID(client *tmux.Client, mgr *session.Manager) string {
	wid, _ := client.GetEnv("ROOST_ACTIVE_WINDOW")
	if wid == "" {
		return ""
	}
	for _, s := range mgr.All() {
		if s.WindowID == wid {
			slog.Info("restored active window", "window", wid)
			return wid
		}
	}
	slog.Info("stale active window, clearing", "window", wid)
	return ""
}

func loadConfig() *config.Config {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	return cfg
}


func printUsage() {
	fmt.Println("roost - AI agent session manager on tmux")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  roost          Start or attach to the roost tmux session")
	for _, pair := range lib.RegisteredHelp() {
		fmt.Printf("  roost %-8s %s\n", pair[0], pair[1])
	}
	fmt.Println("  roost help     Show this help message")
}

func resolveExe() string {
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}
