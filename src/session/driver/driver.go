package driver

import "io/fs"

// SessionMeta holds session metadata resolved by a driver.
type SessionMeta struct {
	Title      string   // session name (e.g. custom-title)
	LastPrompt string   // most recent prompt text
	Subjects   []string // TaskCreate subjects
	Source     string   // log filename used for resolution
}

// Driver defines the interface for agent command-specific behavior.
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	// ResolveMeta resolves session metadata from log files.
	// source is the previously resolved log filename (empty for discovery).
	ResolveMeta(fsys fs.FS, projectPath string, source string) SessionMeta
}
