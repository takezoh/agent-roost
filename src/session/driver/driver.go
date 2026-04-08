package driver

import "io/fs"

// SessionMeta holds session metadata resolved by a driver.
type SessionMeta struct {
	Title      string   // session name (e.g. custom-title)
	LastPrompt string   // most recent prompt text
	Subjects   []string // TaskCreate subjects
	SessionID  string   // agent session ID resolved from log files
}

// Driver defines the interface for agent command-specific behavior.
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	// ResolveMeta resolves session metadata from log files.
	// sessionID identifies which log file to read (empty for auto-discovery).
	ResolveMeta(fsys fs.FS, projectPath string, sessionID string) SessionMeta
}
