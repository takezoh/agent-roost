package driver

import "io/fs"

// SessionMeta holds session metadata resolved by a driver.
type SessionMeta struct {
	Title      string   // session name (e.g. custom-title)
	LastPrompt string   // most recent prompt text
	Subjects   []string // TaskCreate subjects

	// PR5 additions: derived from transcript.SessionInsight.
	AgentName      string         // type=agent-name event (Claude-assigned slug)
	CurrentTool    string         // most recent tool_use awaiting a result
	RecentCommands []string       // recent Bash commands
	SubagentCounts map[string]int // agentType -> launches
	ErrorCount     int            // tool_result is_error count
	TouchedFiles   []string       // unique Read/Write/Edit file paths
}

// Driver defines the interface for agent command-specific behavior.
//
// All session-aware methods take a SessionContext rather than individual
// fields so the core/manager layer never has to know which DriverState keys
// the driver uses. Adding a new driver-specific field is a single-file change
// inside the driver implementation.
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string

	// IdentityKey returns the DriverState key whose value uniquely identifies
	// the agent process for binding (AgentStore.Bind). Drivers without a
	// stable agent identifier return "".
	IdentityKey() string

	// WorkingDir returns the directory the agent process is actually running
	// in (used for git branch detection). Empty falls back to sc.Project.
	WorkingDir(sc SessionContext) string

	// SpawnCommand returns the shell command for (re)starting an agent
	// process. Drivers that support resuming a prior agent session augment
	// the base command (e.g. "claude --resume <id>") by reading their own
	// keys out of sc.DriverState.
	SpawnCommand(baseCommand string, sc SessionContext) string

	// TranscriptFilePath returns the absolute transcript file path the agent
	// is writing for the current session, or "" if the driver has no
	// transcript concept. Implementations decide whether to prefer an
	// agent-reported path stored in sc.DriverState or to compute one.
	TranscriptFilePath(home string, sc SessionContext) string

	// ResolveMeta reads session metadata from the agent's transcript file.
	// Empty/unreadable files yield an empty SessionMeta silently.
	ResolveMeta(fsys fs.FS, home string, sc SessionContext) SessionMeta

	// NewObserver creates a per-session state producer Observer for the given
	// window. The Observer is the sole writer to state.Store for windowID.
	// Construction MUST NOT touch the store — warm-restart paths rely on the
	// persisted status surviving observer creation. Drivers that don't use
	// some of the deps (e.g. event-driven drivers don't need the capturer)
	// simply ignore the unused fields.
	NewObserver(windowID string, deps ObserverDeps) Observer
}
