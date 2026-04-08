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
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	// ResolveMeta reads session metadata from the agent's transcript file at
	// the given absolute path. Empty path or unreadable file yields empty.
	// Drivers without a transcript concept return empty.
	ResolveMeta(fsys fs.FS, transcriptPath string) SessionMeta
	// TranscriptFilePath returns where the agent should be writing its
	// transcript given a working directory and agent session ID. Used as a
	// fallback when the driver hasn't yet reported the path via hook events
	// (cold-boot before first hook). Drivers without transcripts return "".
	TranscriptFilePath(home, workingDir, agentSessionID string) string
	// SpawnCommand returns the shell command for (re)starting an agent
	// process. Drivers that support resuming a prior agent session augment
	// the base command (e.g. "claude --resume <id>"). Empty agentSessionID
	// returns baseCommand unchanged.
	SpawnCommand(baseCommand, agentSessionID string) string
}
