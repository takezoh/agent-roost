package gemini

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/cli"
)

func init() {
	cli.Register("gemini", "Gemini CLI integration (setup)", Run)
}

// Run dispatches Gemini subcommands.
func Run(args []string) error {
	if len(args) == 0 {
		printHelp()
		return fmt.Errorf("gemini: missing subcommand")
	}
	switch args[0] {
	case "setup":
		return runSetup()
	case "help", "-h", "--help":
		printHelp()
		return nil
	default:
		fmt.Fprintf(os.Stderr, "roost gemini: unknown subcommand: %s\n", args[0])
		printHelp()
		return fmt.Errorf("gemini: unknown subcommand: %s", args[0])
	}
}

func printHelp() {
	fmt.Print(`Usage: roost gemini <command>

Commands:
  setup    Register roost hooks in ~/.gemini/settings.json
  help     Show this help message
`)
}

func runSetup() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(home, ".gemini", "settings.json")
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
