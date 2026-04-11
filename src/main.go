package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"


	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/lib"
	_ "github.com/takezoh/agent-roost/lib/claude" // registers the "claude" subcommand
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/runtime"
	"github.com/takezoh/agent-roost/runtime/worker"
	statedriver "github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/tmux"
	"github.com/takezoh/agent-roost/tools"
	"github.com/takezoh/agent-roost/tui"
)

func main() {
	cfg, _ := config.Load()
	level := "info"
	dataDir := ""
	if cfg != nil {
		level = cfg.Log.Level
		dataDir = cfg.ResolveDataDir()
	}
	logger.InitWithDataDir(level, dataDir)
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

	dataDir := cfg.ResolveDataDir()
	os.MkdirAll(dataDir, 0o755)
	home, _ := os.UserHomeDir()

	idleThreshold := time.Duration(cfg.Monitor.IdleThresholdSec) * time.Second
	eventLogDir := filepath.Join(dataDir, "events")
	statedriver.RegisterDefaults(statedriver.RegisterOptions{
		Home:          home,
		EventLogDir:   eventLogDir,
		IdleThreshold: idleThreshold,
		DriverConfigs: cfg.Drivers,
	})

	tmuxBackend := runtime.NewRealTmuxBackend(client)
	pollInterval := time.Duration(cfg.Monitor.PollIntervalMs) * time.Millisecond
	sockPath := filepath.Join(dataDir, "roost.sock")

	statedriver.RegisterRunners(tmuxBackend.CapturePane)
	pool := worker.NewPool(4)

	rt := runtime.New(runtime.Config{
		SessionName:  sessionName,
		RoostExe:     resolveExe(),
		DataDir:      dataDir,
		TickInterval: pollInterval,
		Tmux:         tmuxBackend,
		Persist:      runtime.NewFilePersist(dataDir),
		EventLog:     runtime.NewFileEventLog(dataDir),
		Pool:         pool,
	})

	rt.SetAliases(cfg.Session.Aliases)
	rt.SetDefaultCommand(cfg.Session.DefaultCommand)

	warmRestart := client.SessionExists()
	if warmRestart {
		slog.Info("session exists, restoring")
		restoreSession(client, cfg, sessionName)
		if err := rt.LoadSnapshot(); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		rt.RescueActiveSession(client)
		if err := rt.ReconcileWarm(); err != nil {
			slog.Error("reconcile failed", "err", err)
		}
		rt.DeactivateOnStartup(client)
	} else {
		slog.Info("creating new session")
		setupNewSession(client, cfg, sessionName)
		if err := rt.LoadSnapshot(); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		rt.ClearStaleWindowIDs()
		if err := rt.RecreateAll(); err != nil {
			slog.Error("recreate failed", "err", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := rt.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("runtime exited", "err", err)
		}
	}()

	if err := rt.StartIPC(sockPath); err != nil {
		fmt.Fprintf(os.Stderr, "roost: ipc: %v\n", err)
		os.Exit(1)
	}
	slog.Info("server started", "sock", sockPath)

	// Start the FileRelay so log and session files are pushed to TUI
	// subscribers instead of the TUI polling them.
	relay, err := runtime.NewFileRelay(rt)
	if err != nil {
		slog.Warn("filerelay: start failed, TUI will show backfill only", "err", err)
	} else {
		defer relay.Close()
		relay.WatchLog(logger.LogFilePath())
		rt.SetRelay(relay)
	}

	if warmRestart {
		respawnMainPane(client, sessionName)
	}
	respawnSessionsPane(client, sessionName)
	respawnLogPane(client, sessionName)

	slog.Info("attaching to tmux session")
	client.Attach()

	cancel()
	<-rt.Done()

	if rt.ShutdownRequested() {
		slog.Info("shutdown requested, killing tmux session")
		client.KillSession()
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

const paneLabel = `#{?#{==:#{window_index},0},` +
	`#{?#{==:#{pane_index},0},[MAIN],#{?#{==:#{pane_index},1},[LOG],[SESSIONS]}},` +
	`[#{window_name}]}`

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
	// Split style attributes into separate #[...] blocks to avoid commas
	// inside #[...] being misinterpreted as tmux conditional delimiters.
	k := "#[bold]#[fg=#ebdbb2]"    // key style
	d := "#[nobold]#[fg=#626262]" // description style
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

	// Nested tmux conditionals: window 0 → pane-based hints, else → other.
	return "#{?#{==:#{window_index},0}," +
		"#{?#{==:#{pane_index},0}," + main + "," +
		"#{?#{==:#{pane_index},1}," + log + "," + sessions + "}}," +
		other + "}"
}

func setupKeyBindings(client *tmux.Client, sn string) {
	exePath := resolveExe()
	client.UnbindAllKeys("prefix")
	// Space toggles focus between main pane (0.0) and sidebar (0.2).
	client.BindKey("prefix", "Space",
		"if-shell", "-F", `#{==:#{pane_index},2}`,
		"select-pane -t "+sn+":0.0",
		"select-pane -t "+sn+":0.2")
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

func runMainTUI() {
	cfg := loadConfig()
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		model := tui.NewMainModel(nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "roost: main: %v\n", err)
			os.Exit(1)
		}
		return
	}
	defer client.Close()
	client.Subscribe()

	model := tui.NewMainModel(client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: main: %v\n", err)
		os.Exit(1)
	}
}

func runLogViewer() {
	cfg := loadConfig()
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		model := tui.NewLogModel(logger.LogFilePath(), nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			fmt.Fprintf(os.Stderr, "roost: log: %v\n", err)
			os.Exit(1)
		}
		return
	}
	defer client.Close()
	client.Subscribe()

	model := tui.NewLogModel(logger.LogFilePath(), client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "roost: log: %v\n", err)
		os.Exit(1)
	}
}

func runSessionList() {
	cfg := loadConfig()
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: connect: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()
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
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	slog.Info("palette dial", "sock", sockPath)

	client, err := proto.Dial(sockPath)
	if err != nil {
		slog.Error("palette connect failed", "err", err)
		fmt.Fprintf(os.Stderr, "roost: connect: %v\n", err)
		os.Exit(1)
	}
	slog.Info("palette connected")
	defer client.Close()

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

	reg := tools.DefaultRegistry()
	roots := make([]string, len(cfg.Projects.ProjectRoots))
	for i, r := range cfg.Projects.ProjectRoots {
		roots[i] = config.ExpandPath(r)
	}
	ctx := &tools.ToolContext{
		Client: client,
		Config: tools.ToolConfig{
			DefaultCommand: cfg.Session.DefaultCommand,
			Commands:       cfg.Session.Commands,
			Projects:       cfg.ListProjects(),
			ProjectRoots:   roots,
		},
		Args: prefill,
	}

	model := tui.NewPaletteModel(reg, ctx, toolName)
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
	if cfg.Session.DefaultCommand == "" {
		cfg.Session.DefaultCommand = "shell"
	}
	if len(cfg.Session.Commands) == 0 {
		cfg.Session.Commands = []string{"shell"}
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
