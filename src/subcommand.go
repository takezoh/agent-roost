package main

import (
	"fmt"
	"io"

	"github.com/takezoh/agent-roost/cli"
	_ "github.com/takezoh/agent-roost/event"
	_ "github.com/takezoh/agent-roost/lib/claude"
	_ "github.com/takezoh/agent-roost/lib/codex"
	_ "github.com/takezoh/agent-roost/lib/gemini"
)

type commandKind int

const (
	commandKindCLI commandKind = iota
	commandKindDaemon
	commandKindRoost
)

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
		return fmt.Errorf("unknown tui: missing subcommand")
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
