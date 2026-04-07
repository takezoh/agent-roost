package driver

import "io/fs"

// SessionMeta holds session metadata resolved by a driver.
type SessionMeta struct {
	Title      string // session name (e.g. custom-title)
	LastPrompt string // most recent prompt text
}

// Driver defines the interface for agent command-specific behavior.
type Driver interface {
	Name() string
	PromptPattern() string
	DisplayName() string
	ResolveMeta(fsys fs.FS, projectPath string) SessionMeta
}
