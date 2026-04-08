package driver

import "io/fs"

// Generic implements general-purpose commands like bash, codex, and gemini.
// Prompt pattern targets $, >, ❯ at line end or beginning.
type Generic struct {
	name string
}

const genericPromptPattern = `(?m)(^>|[>$❯]\s*$)`

func (g Generic) Name() string          { return g.name }
func (g Generic) PromptPattern() string { return genericPromptPattern }
func (g Generic) DisplayName() string   { return g.name }

func (g Generic) ResolveMeta(fsys fs.FS, projectPath string, sessionID string) SessionMeta {
	return SessionMeta{}
}

// SpawnCommand returns baseCommand unchanged. Generic drivers do not support
// resuming a prior agent session.
func (g Generic) SpawnCommand(baseCommand, agentSessionID string) string {
	return baseCommand
}

// NewGeneric returns a generic Driver for the given command name.
func NewGeneric(name string) Driver {
	return Generic{name: name}
}
