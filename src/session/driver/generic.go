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

// IdentityKey returns "" — generic drivers have no stable agent identifier.
func (g Generic) IdentityKey() string { return "" }

// WorkingDir returns "" — generic drivers don't track an agent cwd separate
// from the recorded project.
func (g Generic) WorkingDir(sc SessionContext) string { return "" }

// SpawnCommand returns baseCommand unchanged. Generic drivers do not support
// resuming a prior agent session.
func (g Generic) SpawnCommand(baseCommand string, sc SessionContext) string {
	return baseCommand
}

// TranscriptFilePath returns "" — generic agents don't have a JSONL
// transcript roost knows how to locate.
func (g Generic) TranscriptFilePath(home string, sc SessionContext) string { return "" }

// ResolveMeta returns an empty SessionMeta.
func (g Generic) ResolveMeta(fsys fs.FS, home string, sc SessionContext) SessionMeta {
	return SessionMeta{}
}

// NewGeneric returns a generic Driver for the given command name.
func NewGeneric(name string) Driver {
	return Generic{name: name}
}
