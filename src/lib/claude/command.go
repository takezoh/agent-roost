package claude

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/take/agent-roost/config"
	"github.com/take/agent-roost/lib"
	"github.com/take/agent-roost/lib/claude/hookevent"
	"github.com/take/agent-roost/proto"
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

	// Capture timestamp before stdin read. Each hook fires as a
	// separate process; stdin read latency varies with payload size.
	// Timestamping after the read lets a fast-reading later event get
	// a lower timestamp than a slow-reading earlier event, causing the
	// driver to drop the later event as stale.
	bridgeTS := time.Now().UnixNano()
	input, _ := io.ReadAll(os.Stdin)
	event, err := hookevent.ParseHookEvent(input)
	if err != nil {
		return
	}
	if event.SessionID == "" {
		return
	}
	slog.Debug("claude hook received",
		"session", sessionID,
		"hook_event", event.HookEventName,
		"claude_session", event.SessionID,
	)
	cfg, err := config.Load()
	if err != nil {
		return
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		slog.Debug("claude hook: dial failed", "session", sessionID, "sock", sockPath, "err", err)
		return
	}
	defer client.Close()

	// Send the raw hook JSON to the driver. The driver owns all
	// field extraction (prompt, cwd, transcript_path, DeriveState).
	// The bridge only does minimal parsing for routing (SessionID,
	// HookEventName).
	payload := map[string]any{
		"raw":       string(input),
		"bridge_ts": bridgeTS,
	}
	if err := client.SendHook("claude", event.HookEventName, sessionID, payload); err != nil {
		slog.Debug("claude hook: send failed", "session", sessionID, "err", err)
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
