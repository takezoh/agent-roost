package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tools"
	"github.com/takezoh/agent-roost/tui"
)

func runMainTUI() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		model := tui.NewMainModel(nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			return fmt.Errorf("main: %w", err)
		}
		return nil
	}
	defer client.Close()
	client.Subscribe()

	model := tui.NewMainModel(client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("main: %w", err)
	}
	return nil
}

func runLogViewer() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		model := tui.NewLogModel(logger.LogFilePath(), nil)
		if _, err := tea.NewProgram(model).Run(); err != nil {
			return fmt.Errorf("log: %w", err)
		}
		return nil
	}
	defer client.Close()
	client.Subscribe()

	model := tui.NewLogModel(logger.LogFilePath(), client)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("log: %w", err)
	}
	return nil
}

func runSessionList() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")

	client, err := proto.Dial(sockPath)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer client.Close()
	client.Subscribe()

	model := tui.NewModel(client, cfg)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

func runPalette(args []string) error {
	slog.Info("palette start", "args", args)
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	tui.ApplyTheme(cfg.Theme)
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	slog.Info("palette dial", "sock", sockPath)

	client, err := proto.Dial(sockPath)
	if err != nil {
		slog.Error("palette connect failed", "err", err)
		return fmt.Errorf("connect: %w", err)
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

	if toolName == "push-driver" {
		_, activeID, _, err := client.ListSessions()
		if err != nil || activeID == "" {
			fmt.Fprintln(os.Stderr, "no active session")
			time.Sleep(700 * time.Millisecond)
			return nil
		}
		prefill["session_id"] = activeID
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
			PushCommands:   cfg.Session.PushCommands,
			Projects:       cfg.ListProjects(),
			ProjectRoots:   roots,
		},
		Args: prefill,
	}

	model := tui.NewPaletteModel(reg, ctx, toolName)
	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("palette: %w", err)
	}
	return nil
}
