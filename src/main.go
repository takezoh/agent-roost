package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/cli"
	"github.com/takezoh/agent-roost/config"
	_ "github.com/takezoh/agent-roost/event"
	_ "github.com/takezoh/agent-roost/lib/claude"
	_ "github.com/takezoh/agent-roost/lib/codex"
	"github.com/takezoh/agent-roost/logger"
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

	if cli.Dispatch(os.Args[1:]) {
		return
	}

	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help") {
		printUsage()
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "--tui" && len(os.Args) > 2 {
		logger.RedirectStderr()
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
	for _, pair := range cli.RegisteredHelp() {
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
