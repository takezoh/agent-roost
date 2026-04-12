package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/cli"
	"github.com/takezoh/agent-roost/config"
	_ "github.com/takezoh/agent-roost/event"
	_ "github.com/takezoh/agent-roost/lib/claude"
	_ "github.com/takezoh/agent-roost/lib/codex"
	_ "github.com/takezoh/agent-roost/lib/gemini"
	"github.com/takezoh/agent-roost/logger"
)

type commandKind int

const (
	commandKindCLI commandKind = iota
	commandKindDaemon
	commandKindRoost
)

var (
	loadBootstrapConfig   = config.Load
	initLoggerWithDataDir = logger.InitWithDataDir
	closeLogger           = logger.Close
	redirectStderr        = logger.RedirectStderr
	runCoordinatorFn      = runCoordinator
	runMainTUIFn          = runMainTUI
	runSessionListFn      = runSessionList
	runLogViewerFn        = runLogViewer
	runPaletteFn          = runPalette
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(args []string, stdout, stderr io.Writer) (code int) {
	kind := classifyCommand(args)
	cfg, cfgErr := loadBootstrapConfig()
	loggerReady, loggerErr := initMainLogger(cfg)
	if loggerReady {
		defer closeLogger()
	}
	defer func() {
		if rec := recover(); rec != nil {
			err := fmt.Errorf("panic: %v", rec)
			if loggerReady {
				slog.Error("panic recovered", "err", err)
			}
			code = finishMain(kind, err, loggerReady, loggerErr, stdout, stderr)
		}
	}()

	if loggerErr != nil {
		return finishMain(kind, loggerErr, false, loggerErr, stdout, stderr)
	}
	if cfgErr != nil {
		slog.Error("config load failed during logger bootstrap", "err", cfgErr)
	}

	err := runCommand(args, stdout)
	if err != nil {
		slog.Error("main failed", "err", err)
	}
	return finishMain(kind, err, true, nil, stdout, stderr)
}

func finishMain(kind commandKind, err error, loggerReady bool, loggerErr error, stdout, stderr io.Writer) int {
	if kind == commandKindRoost {
		if err != nil {
			fmt.Fprintf(stderr, "roost: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "roost: exited")
		return 0
	}
	if !loggerReady && loggerErr != nil {
		return 1
	}
	if err != nil {
		return 1
	}
	return 0
}

func initMainLogger(cfg *config.Config) (bool, error) {
	level := "info"
	dataDir := ""
	if cfg != nil {
		level = cfg.Log.Level
		dataDir = cfg.ResolveDataDir()
	}
	if err := initLoggerWithDataDir(level, dataDir); err != nil {
		return false, err
	}
	return true, nil
}

func classifyCommand(args []string) commandKind {
	if len(args) == 0 {
		return commandKindRoost
	}
	if args[0] == "--tui" {
		return commandKindDaemon
	}
	if isHelpCommand(args[0]) {
		return commandKindCLI
	}
	if cli.Has(args[0]) {
		return commandKindCLI
	}
	return commandKindRoost
}

func runCommand(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return runCoordinatorFn()
	}
	if isHelpCommand(args[0]) {
		printUsage(stdout)
		return nil
	}
	if args[0] == "--tui" {
		redirectStderr()
		return runTUI(args[1:])
	}
	handled, err := cli.Dispatch(args)
	if handled {
		return err
	}
	return runCoordinatorFn()
}

func runTUI(args []string) error {
	if len(args) == 0 {
		return errors.New("unknown tui: missing subcommand")
	}
	switch args[0] {
	case "main":
		return runMainTUIFn()
	case "sessions":
		return runSessionListFn()
	case "log":
		return runLogViewerFn()
	case "palette":
		return runPaletteFn(args[1:])
	default:
		return fmt.Errorf("unknown tui: %s", args[0])
	}
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if cfg.Session.DefaultCommand == "" {
		cfg.Session.DefaultCommand = "shell"
	}
	if len(cfg.Session.Commands) == 0 {
		cfg.Session.Commands = []string{"shell"}
	}
	return cfg, nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "roost - AI agent session manager on tmux")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  roost          Start or attach to the roost tmux session")
	for _, pair := range cli.RegisteredHelp() {
		fmt.Fprintf(w, "  roost %-8s %s\n", pair[0], pair[1])
	}
	fmt.Fprintln(w, "  roost help     Show this help message")
}

func isHelpCommand(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func resolveExe() string {
	exe, _ := os.Executable()
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}
