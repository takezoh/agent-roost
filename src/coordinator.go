package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/takezoh/agent-roost/connector"
	statedriver "github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/features"
	libnotify "github.com/takezoh/agent-roost/lib/notify"
	"github.com/takezoh/agent-roost/lib/tmux"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/runtime"
	"github.com/takezoh/agent-roost/runtime/worker"
	"github.com/takezoh/agent-roost/state"
)

func runCoordinator() error { //nolint:funlen
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	sessionName := cfg.Tmux.SessionName
	slog.Info("starting coordinator", "session", sessionName)
	client := tmux.NewClient(sessionName)

	dataDir := cfg.ResolveDataDir()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}
	home, _ := os.UserHomeDir()

	idleThreshold := time.Duration(cfg.Monitor.IdleThresholdSec) * time.Second
	eventLogDir := filepath.Join(dataDir, "events")
	statedriver.RegisterDefaults(statedriver.RegisterOptions{
		Home:             home,
		EventLogDir:      eventLogDir,
		IdleThreshold:    idleThreshold,
		DriverConfigs:    cfg.Drivers,
		SummarizeCommand: cfg.Driver.SummarizeCommand,
		Pager:            cfg.Driver.Pager,
	})

	tmuxBackend := runtime.NewRealTmuxBackend(client)
	pollInterval := time.Duration(cfg.Monitor.PollIntervalMs) * time.Millisecond
	fastPollInterval := time.Duration(cfg.Monitor.FastPollIntervalMs) * time.Millisecond
	sockPath := filepath.Join(dataDir, "roost.sock")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	terminalEvict := statedriver.RegisterRunners(tmuxBackend.CapturePaneEscaped, cfg.Driver.SummarizeCommand)
	connector.RegisterDefaults()
	connector.RegisterRunners()
	pool := worker.NewPool(ctx, 4)

	ln, err := libnotify.New(ctx, dataDir)
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}

	tapDir := filepath.Join(dataDir, "tap")
	if err := os.MkdirAll(tapDir, 0o755); err != nil {
		return fmt.Errorf("mkdir tap dir: %w", err)
	}
	paneTap := runtime.NewTmuxPipePaneTap(tmuxBackend.PipePane, tapDir)

	featureSet := features.FromConfig(cfg.Features.Enabled, features.All())
	rt := runtime.New(runtime.Config{
		SessionName:       sessionName,
		RoostExe:          resolveExe(),
		DataDir:           dataDir,
		TickInterval:      pollInterval,
		FastTickInterval:  fastPollInterval,
		MainPaneHeightPct: cfg.Tmux.PaneRatioVertical,
		Tmux:              tmuxBackend,
		Persist:           runtime.NewFilePersist(dataDir),
		EventLog:          runtime.NewFileEventLog(dataDir),
		ToolLog:           runtime.NewFileToolLog(dataDir),
		Pool:              pool,
		Notifier:          runtime.NewNotifier(&cfg.Notifications, ln),
		TerminalEvict:     terminalEvict,
		Tap:               paneTap,
		Features:          featureSet,
	})

	rt.SetAliases(cfg.Session.Aliases)
	rt.SetDefaultCommand(cfg.Session.DefaultCommand)

	warmRestart := client.SessionExists()
	if warmRestart {
		slog.Info("session exists, restoring")
		ensureHiddenWindow(client, sessionName)
		state.Register(statedriver.NewShellDriver("shell", resolveShellDisplay(client), idleThreshold))
		if err := rt.LoadSnapshot(false); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		if err := rt.LoadSessionPanes(); err != nil {
			slog.Warn("window map load failed", "err", err)
		}
		rt.RecoverActivePaneAtMain()
		restoreSession(client, cfg, sessionName)
		rt.ReconcileOrphans()
		rt.RecoverWarmStartSessions()
	} else {
		slog.Info("creating new session")
		if err := setupNewSession(client, cfg, sessionName); err != nil {
			return err
		}
		state.Register(statedriver.NewShellDriver("shell", resolveShellDisplay(client), idleThreshold))
		if err := rt.LoadSessionPanes(); err != nil {
			slog.Warn("window map load failed", "err", err)
		}
		if err := rt.LoadSnapshot(true); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		if err := rt.RecreateAll(); err != nil {
			slog.Error("recreate failed", "err", err)
		}
	}

	runErrCh := make(chan error, 1)
	go func() {
		if err := rt.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			runErrCh <- err
		}
	}()

	rt.StartTapsForRestoredFrames()

	if err := rt.StartIPC(sockPath); err != nil {
		return fmt.Errorf("ipc: %w", err)
	}
	slog.Info("server started", "sock", sockPath)

	if relay, err := runtime.NewFileRelay(rt); err != nil {
		slog.Warn("filerelay: start failed, TUI will show backfill only", "err", err)
	} else {
		defer relay.Close()
		relay.WatchLog(logger.LogFilePath())
		rt.SetRelay(relay)
	}

	rt.RespawnMainPane()
	respawnHeaderPane(client, sessionName)
	respawnSessionsPane(client, sessionName)
	// respawnHiddenPane must come after StartIPC: the log TUI dials the
	// Unix socket on startup, and the socket does not exist until StartIPC
	// returns. Any earlier respawn results in a silent offline-mode boot.
	respawnHiddenPane(client, sessionName)

	slog.Info("attaching to tmux session")
	if err := client.Attach(); err != nil {
		slog.Warn("attach exited", "err", err)
	}

	cancel()
	<-rt.Done()
	close(runErrCh)
	if err, ok := <-runErrCh; ok {
		return fmt.Errorf("runtime: %w", err)
	}

	if client.SessionExists() {
		slog.Info("detached, session kept alive")
	} else {
		slog.Info("tmux server exited")
	}
	return nil
}

// resolveShellDisplayFromValues picks the display name (basename) for the
// shell driver. Pure function — used directly by tests.
func resolveShellDisplayFromValues(tmuxDefault, envSHELL string) string {
	for _, raw := range []string{tmuxDefault, envSHELL} {
		if name := filepath.Base(raw); name != "" && name != "." && name != "/" {
			return name
		}
	}
	return "shell"
}

// resolveShellDisplay queries tmux's default-shell option (the shell tmux
// will actually spawn for login-shell panes) and falls back to $SHELL.
func resolveShellDisplay(client *tmux.Client) string {
	tmuxDefault, _ := client.ShowOption("default-shell")
	return resolveShellDisplayFromValues(tmuxDefault, os.Getenv("SHELL"))
}
