package driver

import (
	"sync"
	"time"

	"github.com/take/agent-roost/state"
)

// RegisterDefaults wires the built-in driver set into the global
// state registry. Idempotent — repeated calls are no-ops so test
// binaries that import multiple sub-packages don't double-register.
//
// Called from main() during runtime startup. The runtime needs both
// the home directory (for Claude transcript path resolution) and the
// idle threshold (for the generic driver's polling timeout).
func RegisterDefaults(home, eventLogDir string, idleThreshold time.Duration) {
	registerOnce.Do(func() {
		state.Register(NewClaudeDriver(home, eventLogDir))
		state.Register(NewGenericDriver("bash", idleThreshold))
		state.Register(NewGenericDriver("codex", idleThreshold))
		state.Register(NewGenericDriver("gemini", idleThreshold))
		state.Register(NewGenericDriver("shell", idleThreshold))
		// Fallback driver under the empty name. Sessions whose
		// command isn't a registered key will route here.
		state.Register(NewGenericDriver("", idleThreshold))
	})
}

var registerOnce sync.Once
