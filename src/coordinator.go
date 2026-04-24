package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/connector"
	statedriver "github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/features"
	libnotify "github.com/takezoh/agent-roost/lib/notify"
	"github.com/takezoh/agent-roost/lib/tmux"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/runtime"
	"github.com/takezoh/agent-roost/runtime/worker"
	sandboxdocker "github.com/takezoh/agent-roost/sandbox/docker"
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
	agentLauncher, err := newAgentLauncher(cfg.Sandbox)
	if err != nil {
		return err
	}
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
		Launcher:          agentLauncher,
	})

	rt.SetAliases(cfg.Session.Aliases)
	rt.SetDefaultCommand(cfg.Session.DefaultCommand)

	pruneOrphans := func(knownProjects []string) {
		if d, ok := agentLauncher.(*runtime.SandboxDispatcher); ok {
			pruneCtx, pruneCancel := context.WithTimeout(ctx, 30*time.Second)
			defer pruneCancel()
			d.PruneOrphans(pruneCtx, knownProjects)
		}
	}

	warmRestart := client.SessionExists()
	if warmRestart {
		slog.Info("session exists, restoring")
		ensureHiddenWindow(client, sessionName)
		state.Register(statedriver.NewShellDriver("shell", resolveShellDisplay(client), idleThreshold))
		if err := rt.LoadSnapshot(false); err != nil {
			slog.Error("snapshot load failed", "err", err)
		}
		pruneOrphans(rt.KnownProjects())
		if err := rt.LoadSessionPanes(); err != nil {
			slog.Warn("window map load failed", "err", err)
		}
		rt.RecoverActivePaneAtMain()
		restoreSession(client, cfg, sessionName)
		rt.ReconcileOrphans()
		rt.RecoverSandboxFrames()
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
		pruneOrphans(rt.KnownProjects())
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

// newAgentLauncher returns the AgentLauncher for the configured sandbox mode.
// Returns a SandboxDispatcher that routes each launch to direct or docker based
// on the effective config for that project (user scope + optional project scope).
// Returns an error when user scope mode="docker" but the docker daemon is unreachable.
func newAgentLauncher(sb config.SandboxConfig) (runtime.AgentLauncher, error) {
	resolver := config.NewSandboxResolver(sb)
	d := &runtime.SandboxDispatcher{
		Resolver: resolver,
		Direct:   runtime.DirectLauncher{},
	}
	if sb.Mode == "docker" {
		if err := checkDockerAvailable(); err != nil {
			return nil, fmt.Errorf(
				"sandbox.mode=docker but docker is unavailable: %w\n"+
					"  → set sandbox.mode=direct in ~/.roost/settings.toml or fix docker", err)
		}
		mgr := sandboxdocker.New(sandboxdocker.Config{
			Network:   sb.Docker.Network,
			ExtraArgs: sb.Docker.ExtraArgs,
		})
		d.Docker = runtime.NewDockerLauncher(mgr, func(project string) config.DockerConfig {
			return resolver.Resolve(project).Docker
		})
		slog.Info("sandbox: docker backend enabled")
	}
	return d, nil
}

// checkDockerAvailable verifies that the docker daemon is reachable by running
// "docker info". Returns a non-nil error when docker is missing or the daemon
// is not running.
func checkDockerAvailable() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker info: %w\n%s", err, string(out))
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
