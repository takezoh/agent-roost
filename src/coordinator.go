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
	"github.com/takezoh/agent-roost/lib/tmux"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/runtime"
	"github.com/takezoh/agent-roost/runtime/worker"
)

func runCoordinator() error {
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
		Home:          home,
		EventLogDir:   eventLogDir,
		IdleThreshold: idleThreshold,
		DriverConfigs: cfg.Drivers,
	})

	tmuxBackend := runtime.NewRealTmuxBackend(client)
	pollInterval := time.Duration(cfg.Monitor.PollIntervalMs) * time.Millisecond
	sockPath := filepath.Join(dataDir, "roost.sock")

	claudeOpts := statedriver.ParseClaudeOptions(cfg.Drivers[statedriver.ClaudeDriverName])
	statedriver.RegisterRunners(tmuxBackend.CapturePane, cfg.Language, claudeOpts.SummarizeCommand)
	connector.RegisterDefaults()
	connector.RegisterRunners()
	pool := worker.NewPool(4)

	rt := runtime.New(runtime.Config{
		SessionName:       sessionName,
		RoostExe:          resolveExe(),
		DataDir:           dataDir,
		TickInterval:      pollInterval,
		MainPaneHeightPct: cfg.Tmux.PaneRatioVertical,
		Tmux:              tmuxBackend,
		Persist:           runtime.NewFilePersist(dataDir),
		EventLog:          runtime.NewFileEventLog(dataDir),
		Pool:              pool,
	})

	rt.SetAliases(cfg.Session.Aliases)
	rt.SetDefaultCommand(cfg.Session.DefaultCommand)

	warmRestart := client.SessionExists()
	if warmRestart {
		slog.Info("session exists, restoring")
		if err := rt.LoadSnapshot(); err != nil {
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
		if err := rt.LoadSessionPanes(); err != nil {
			slog.Warn("window map load failed", "err", err)
		}
		if err := rt.LoadSnapshot(); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		if err := rt.RecreateAll(); err != nil {
			slog.Error("recreate failed", "err", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		if err := rt.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			runErrCh <- err
		}
	}()

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
	respawnSessionsPane(client, sessionName)
	respawnLogPane(client, sessionName)

	slog.Info("attaching to tmux session")
	if err := client.Attach(); err != nil {
		slog.Warn("attach exited", "err", err)
	}

	rt.DeactivateBeforeExit()
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
