package driver

import (
	"time"
)

// Driver is the per-session stateful agent abstraction. Each Driver instance
// belongs to exactly one session and is the sole producer + reader of that
// session's dynamic state (status / title / lastPrompt / insight / identity).
//
// Drivers receive window I/O through the WindowInfo interface that
// Coordinator passes to Tick() — they never import session/ or tmux/. Hook
// events arrive through HandleEvent(). Persistent identity (the driver's
// opaque key/value bag) flows through PersistedState() / RestorePersistedState().
type Driver interface {
	// Static identity
	Name() string
	DisplayName() string

	// Lifecycle
	MarkSpawned()                       // fresh agent process just started
	Tick(now time.Time, win WindowInfo) // periodic poll (event drivers no-op)
	HandleEvent(ev AgentEvent) bool     // hook event arrived
	Close()                             // session destroyed; release resources

	// Status returns the current state machine value. Used by core for
	// generic state-aware logic (filters, state-changed-at, etc.).
	Status() (StatusInfo, bool)

	// View returns the complete TUI payload for this session. The driver
	// owns all UI content (Card / LogTabs / InfoExtras / StatusLine).
	// View() is a pure getter — heavy work belongs in Tick / HandleEvent.
	View() SessionView

	// Persistence (driver-defined opaque bag round-tripped through tmux user
	// options + sessions.json by SessionService).
	PersistedState() map[string]string
	RestorePersistedState(state map[string]string)

	// SpawnCommand returns the shell command for (re)starting the agent
	// process during cold-boot recovery. Drivers that support resume augment
	// the base command using their own keys recovered from PersistedState.
	SpawnCommand(baseCommand string) string

	// Atomic runs fn with the underlying driver in a single critical
	// section. In production this collapses to one driverActor inbox
	// round-trip even when fn invokes several Driver methods, so callers
	// can read multiple fields (e.g. PersistedState + View().StatusLine
	// after a HandleEvent) without paying for one round-trip per call.
	//
	// fn must NOT call back into the Coordinator or any other actor that
	// could be waiting on this Driver — doing so would deadlock the
	// driverActor goroutine. The contract is "pure read/write of this
	// driver's own state".
	Atomic(fn func(d Driver))
}

// AgentEvent is defined in event.go. HandleEvent receives this driver-neutral
// payload and is responsible for picking out the keys (in DriverState) that
// matter to it.

// Driver-side per-session state (session id, active flag) is delivered
// without any back-reference to core/ — the session id is captured at
// construction via Deps.SessionID, and the active flag is pushed at
// dispatch time via WindowInfo.Active(). The Coordinator actor builds
// the WindowInfo snapshot before fanning Tick out, so a Driver actor
// never needs to call back into the Coordinator (which would deadlock
// against the actor model).
