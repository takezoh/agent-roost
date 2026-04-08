package claude

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/lib"
)

func init() {
	lib.Register("claude", "Claude Code integration (setup, event)", Run)
}

// Run dispatches Claude subcommands (event, setup).
func Run(args []string) {
	if len(args) == 0 {
		printHelp()
		os.Exit(1)
	}
	switch args[0] {
	case "event":
		runEvent()
	case "setup":
		runSetup()
	case "help", "-h", "--help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "roost claude: unknown subcommand: %s\n", args[0])
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Print(`Usage: roost claude <command>

Commands:
  setup    Register roost hooks in ~/.claude/settings.json
  event    Receive a hook event from Claude Code (called by hooks)
  help     Show this help message
`)
}

func runEvent() {
	input, _ := io.ReadAll(os.Stdin)
	event, err := ParseHookEvent(input)
	if err != nil {
		return
	}
	if event.SessionID == "" {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := core.Dial(sockPath)
	if err != nil {
		return
	}
	defer client.Close()
	client.StartListening()

	if event.HookEventName == "SessionStart" {
		args := map[string]string{
			"session_id": event.SessionID,
		}
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			args["pane"] = pane
		}
		client.SendAgentEvent("session-start", args)
	}

	if state := event.DeriveState(); state != "" {
		args := map[string]string{
			"session_id": event.SessionID,
			"state":      state,
			"log":        event.FormatLog(),
		}
		if pane := os.Getenv("TMUX_PANE"); pane != "" {
			args["pane"] = pane
		}
		client.SendAgentEvent("state-change", args)
	}
}

func runSetup() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	roostPath, _ := os.Executable()
	if resolved, err := filepath.EvalSymlinks(roostPath); err == nil {
		roostPath = resolved
	}
	events, err := RegisterHooks(settingsPath, roostPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "roost: %v\n", err)
		os.Exit(1)
	}
	if len(events) == 0 {
		fmt.Println("Hooks already registered")
		return
	}
	fmt.Printf("Registered events: %v\n", events)
	fmt.Printf("  Settings: %s\n", settingsPath)
}
