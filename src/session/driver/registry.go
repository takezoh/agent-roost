package driver

import "regexp"

// Registry is an immutable map for looking up a Driver by command name.
// Returns a fallback Driver for unknown commands (never returns nil).
type Registry struct {
	drivers  map[string]Driver
	patterns map[string]*regexp.Regexp
	fallback Driver
}

// NewRegistry returns a Registry with the given drivers registered.
// The fallback is used for unknown commands.
func NewRegistry(drivers []Driver, fallback Driver) *Registry {
	r := &Registry{
		drivers:  make(map[string]Driver, len(drivers)),
		patterns: make(map[string]*regexp.Regexp, len(drivers)+1),
		fallback: fallback,
	}
	for _, d := range drivers {
		r.drivers[d.Name()] = d
		r.patterns[d.Name()] = regexp.MustCompile(d.PromptPattern())
	}
	r.patterns[""] = regexp.MustCompile(fallback.PromptPattern())
	return r
}

// Get returns the Driver for the given command line. The command is parsed
// via Kind so invocations like "claude --worktree" or "FOO=bar claude" still
// resolve to the registered "claude" driver. Unknown commands return fallback.
func (r *Registry) Get(command string) Driver {
	if d, ok := r.drivers[Kind(command)]; ok {
		return d
	}
	return r.fallback
}

// CompiledPattern returns the compiled regexp for the given command line.
// Unknown commands return the fallback pattern.
func (r *Registry) CompiledPattern(command string) *regexp.Regexp {
	if p, ok := r.patterns[Kind(command)]; ok {
		return p
	}
	return r.patterns[""]
}

// DefaultRegistry returns a Registry for known commands.
func DefaultRegistry() *Registry {
	drivers := []Driver{
		Claude{},
		NewGeneric("gemini"),
		NewGeneric("codex"),
		NewGeneric("bash"),
	}
	return NewRegistry(drivers, NewGeneric(""))
}
