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

	claudeOpts := statedriver.ParseClaudeOptions(cfg.Drivers["claude"])
	statedriver.RegisterRunners(tmuxBackend.CapturePane, cfg.Language, claudeOpts.SummarizeCommand)
	connector.RegisterDefaults()
	connector.RegisterRunners()
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
		if err := rt.LoadWindowMap(); err != nil {
			slog.Warn("window map load failed", "err", err)
		}
		rt.ReconcileOrphans()
	} else {
		slog.Info("creating new session")
		setupNewSession(client, cfg, sessionName)
		if err := rt.LoadSnapshot(); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
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

	relay, err := runtime.NewFileRelay(rt)
	if err != nil {
		slog.Warn("filerelay: start failed, TUI will show backfill only", "err", err)
	} else {
		defer relay.Close()
		relay.WatchLog(logger.LogFilePath())
		rt.SetRelay(relay)
	}

	respawnMainPane(client, sessionName)
	respawnSessionsPane(client, sessionName)
	respawnLogPane(client, sessionName)

	slog.Info("attaching to tmux session")
	client.Attach()

	rt.DeactivateBeforeExit()
	cancel()
	<-rt.Done()

	if rt.ShutdownRequested() {
		slog.Info("shutdown requested, killing tmux session")
		client.KillSession()
	} else {
		slog.Info("detached, session kept alive")
	}
}
