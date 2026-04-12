package claude

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/cli"
)

func init() {
	cli.Register("claude", "Claude Code integration (setup)", Run)
}

// Run dispatches Claude subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return fmt.Errorf("claude: missing subcommand")
	}
	switch args[0] {
	case "setup":
		return runSetup()
	case "help", "-h", "--help":
		printHelp()
		return nil
	default:
		fmt.Fprintf(os.Stderr, "roost claude: unknown subcommand: %s\n", args[0])
		printHelp()
		return fmt.Errorf("claude: unknown subcommand: %s", args[0])
	}
}

func printHelp() {
	fmt.Print(`Usage: roost claude <command>

Commands:
  setup    Register roost hooks in ~/.claude/settings.json
  help     Show this help message
`)
}

func runSetup() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	roostPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(roostPath); err == nil {
		roostPath = resolved
	}
	events, err := RegisterHooks(settingsPath, roostPath)
	if err != nil {
		return err
	}
	if len(events) == 0 {
		fmt.Println("Hooks already registered")
		return nil
	}
	fmt.Printf("Registered events: %v\n", events)
	fmt.Printf("  Settings: %s\n", settingsPath)
	return nil
}
