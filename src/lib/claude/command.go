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
	"github.com/take/agent-roost/tmux"
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
	// Hooks are registered globally in ~/.claude/settings.json, so this
	// command can be invoked by Claude instances running anywhere — outside
	// tmux entirely, or inside an unrelated tmux server/session/window. Only
	// events from a roost-managed window should mutate roost state, so bail
	// out before reading stdin / dialing the coordinator if the caller's pane
	// has no @roost_id user option.
	pane, ok := currentRoostPane()
	if !ok {
		return
	}

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

// currentRoostPane returns the caller's $TMUX_PANE if and only if that pane
// belongs to a roost-managed window (i.e. carries the @roost_id user option).
// It returns ("", false) when invoked outside tmux, or from any tmux pane that
// roost did not create.
func currentRoostPane() (string, bool) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return "", false
	}
	// SessionName is irrelevant for `display-message -t <pane>`, so an empty
	// client works without loading config.
	out, err := tmux.NewClient("").DisplayMessage(pane, "#{@roost_id}")
	if err != nil || out == "" {
		return "", false
	}
	return pane, true
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
