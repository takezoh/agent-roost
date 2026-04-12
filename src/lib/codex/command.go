package codex

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/cli"
)

func init() {
	cli.Register("codex", "Codex CLI integration (setup)", Run)
}

func Run(args []string) {
	if len(args) == 0 {
		printHelp()
		os.Exit(1)
	}
	switch args[0] {
	case "setup":
		runSetup()
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "roost codex: unknown subcommand: %s\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`Usage: roost codex <command>

Commands:
  setup    Enable hooks in ~/.codex/config.toml and register roost hook handlers
  help     Show this help message
`)
}

func runSetup() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	cfgPath := filepath.Join(home, ".codex", "config.toml")
	hooksPath := filepath.Join(home, ".codex", "hooks.json")

	roostPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(roostPath); err == nil {
		roostPath = resolved
	}
	updated, events, err := RegisterHooks(cfgPath, hooksPath, roostPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	if !updated {
		fmt.Println("Codex hooks already configured")
		return
	}
	fmt.Printf("Configured Codex hooks: %v\n", events)
	fmt.Printf("  Config: %s\n", cfgPath)
	fmt.Printf("  Hooks:  %s\n", hooksPath)
}
