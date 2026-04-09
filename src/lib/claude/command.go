package claude

import (
	"fmt"
	"io"
	"log/slog"
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
	// Hooks are registered globally in ~/.claude/settings.json, so this
	// command can be invoked by Claude instances running anywhere — outside
	// tmux entirely, or inside an unrelated tmux server/session/window. Only
	// events from a roost-managed window should mutate roost state, so bail
	// out before reading stdin / dialing the coordinator if the caller's
	// environment has no ROOST_SESSION_ID (set atomically by SessionService
	// via tmux new-window -e).
	sessionID, ok := currentRoostSessionID()
	if !ok {
		return
	}

	input, _ := io.ReadAll(os.Stdin)
	event, err := ParseHookEvent(input)
	if err != nil {
		slog.Warn("claude hook: parse failed", "session", sessionID, "err", err)
		return
	}
	if event.SessionID == "" {
		slog.Debug("claude hook: missing claude session_id", "session", sessionID, "hook_event", event.HookEventName)
		return
	}
	slog.Debug("claude hook received",
		"session", sessionID,
		"hook_event", event.HookEventName,
		"notification_type", event.NotificationType,
		"tool", event.ToolName,
		"claude_session", event.SessionID,
	)
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("claude hook: config load failed", "session", sessionID, "err", err)
		return
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := core.Dial(sockPath)
	if err != nil {
		slog.Warn("claude hook: dial failed", "session", sessionID, "sock", sockPath, "err", err)
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
		if err := client.SendAgentEvent(driver.AgentEvent{
			Type:        driver.AgentEventSessionStart,
			SessionID:   sessionID,
			DriverState: state,
		}); err != nil {
			slog.Warn("claude hook: send SessionStart failed", "session", sessionID, "err", err)
		}
	}

	hookState := event.DeriveState()
	if hookState == "" {
		slog.Debug("claude hook: no state mapping",
			"session", sessionID,
			"hook_event", event.HookEventName,
			"notification_type", event.NotificationType)
		return
	}
	if err := client.SendAgentEvent(driver.AgentEvent{
		Type:        driver.AgentEventStateChange,
		SessionID:   sessionID,
		State:       hookState,
		Log:         event.FormatLog(),
		DriverState: state,
	}); err != nil {
		slog.Warn("claude hook: send StateChange failed",
			"session", sessionID, "hook_event", event.HookEventName,
			"state", hookState, "err", err)
	}
}

// currentRoostSessionID returns the roost session id this hook bridge is
// running under, by reading the ROOST_SESSION_ID environment variable that
// SessionService injects via `tmux new-window -e`. This is the only legitimate
// way for an in-process caller to identify itself: env vars are set atomically
// at window creation, so they're race-free against the subsequent
// SetWindowUserOptions call. Hook events from any other claude process
// (started outside roost, or in a window roost did not create) are dropped.
func currentRoostSessionID() (string, bool) {
	id := os.Getenv("ROOST_SESSION_ID")
	if id == "" {
		return "", false
	}
	return id, true
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
