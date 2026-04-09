package driver

import (
	"path/filepath"
	"time"

	"github.com/mattn/go-shellwords"
)

// Deps is the dependency bag every driver factory receives. Individual
// drivers ignore the fields they don't need (claudeDriver doesn't poll,
// genericDriver doesn't read transcripts).
//
// Drivers are free to import packages from lib/ directly (e.g. lib/git,
// lib/claude/transcript). Deps only carries values that vary per process
// (IdleThreshold, EventLogDir) or per session (SessionID) — utilities
// with a sensible default are NOT funneled through Deps.
type Deps struct {
	IdleThreshold time.Duration
	Home          string // user home dir for ~/.claude/projects/... resolution
	SessionID     string // per-session id; cached by drivers that own session-scoped resources (event log file, etc.)
	EventLogDir   string // base directory for driver-managed event log files (e.g. claudeDriver writes <EventLogDir>/<sessionID>.log)
}

// Factory constructs a fresh Driver instance for one session. The instance
// owns its private state (status / title / lastPrompt / insight / identity).
// Construction MUST NOT touch external I/O — Restore() is the only place
// that writes back into a Driver after creation.
type Factory func(deps Deps) Driver

// Registry maps a command kind (the executable basename, e.g. "claude") to
// its Factory. Unknown commands fall back to the registered fallback Factory
// (genericDriver). Get / Resolve never return nil.
type Registry struct {
	factories map[string]Factory
	fallback  Factory
}

func NewRegistry(fallback Factory) *Registry {
	return &Registry{
		factories: make(map[string]Factory),
		fallback:  fallback,
	}
}

func (r *Registry) Register(name string, f Factory) {
	r.factories[name] = f
}

// Resolve picks the Factory matching the canonical kind of command. Unknown
// commands return the fallback factory.
func (r *Registry) Resolve(command string) Factory {
	if f, ok := r.factories[Kind(command)]; ok {
		return f
	}
	return r.fallback
}

// DisplayName returns the user-facing label for the command's driver. Used
// by the TUI for the session card chip — kept on Registry so the TUI doesn't
// have to materialize a Driver instance just to render a label.
func (r *Registry) DisplayName(command string) string {
	switch Kind(command) {
	case "claude":
		return "claude"
	case "":
		return ""
	default:
		return Kind(command)
	}
}

// Kind returns the canonical driver name for a raw command string.
// It strips leading KEY=VALUE env assignments and any path prefix on the
// executable, so "FOO=bar /usr/local/bin/claude --worktree" yields "claude".
// Returns "" for empty or unparseable input.
func Kind(rawCommand string) string {
	if rawCommand == "" {
		return ""
	}
	_, args, err := shellwords.ParseWithEnvs(rawCommand)
	if err != nil || len(args) == 0 {
		return ""
	}
	return filepath.Base(args[0])
}

// DefaultRegistry returns a Registry pre-populated with the built-in drivers.
// claude → claudeDriver, gemini/codex/bash → genericDriver, fallback →
// genericDriver. Tests can build their own Registry to inject mocks.
func DefaultRegistry() *Registry {
	r := NewRegistry(newGenericFactory(""))
	r.Register("claude", newClaudeFactory())
	r.Register("gemini", newGenericFactory("gemini"))
	r.Register("codex", newGenericFactory("codex"))
	r.Register("bash", newGenericFactory("bash"))
	return r
}
