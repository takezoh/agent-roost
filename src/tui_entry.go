package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/logger"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/tools"
	"github.com/takezoh/agent-roost/tui"
)

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
