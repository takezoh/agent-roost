package claude

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/core"
	"github.com/take/agent-roost/lib"
	"github.com/take/agent-roost/session/driver"
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

	// Translate the Claude-specific hook payload (cwd, transcript_path, ...)
	// into the driver-neutral AgentEvent the coordinator understands. The
	// keys below are part of the Claude driver's contract — this is the only
	// place outside session/driver/claude.go that knows them, and the
	// coordinator forwards the bag to the driver without inspection.
	pane := os.Getenv("TMUX_PANE")
	state := claudeDriverState(event)

	if event.HookEventName == "SessionStart" {
		client.SendAgentEvent(driver.AgentEvent{
			Type:        driver.AgentEventSessionStart,
			Pane:        pane,
			DriverState: state,
		})
	}

	if hookState := event.DeriveState(); hookState != "" {
		client.SendAgentEvent(driver.AgentEvent{
			Type:        driver.AgentEventStateChange,
			State:       hookState,
			Pane:        pane,
			Log:         event.FormatLog(),
			DriverState: state,
		})
	}
}

// claudeDriverState packs the relevant Claude hook fields into the
// driver-state bag the coordinator persists onto the session. Empty fields
// are omitted so MergeDriverState's "empty value deletes the key" semantics
// don't accidentally clear previously-set entries.
func claudeDriverState(event HookEvent) map[string]string {
	state := map[string]string{
		"session_id": event.SessionID,
	}
	if event.Cwd != "" {
		state["working_dir"] = event.Cwd
	}
	if event.TranscriptPath != "" {
		state["transcript_path"] = event.TranscriptPath
	}
	return state
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
