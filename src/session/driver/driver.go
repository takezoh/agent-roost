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
}

// AgentEvent is defined in event.go. HandleEvent receives this driver-neutral
// payload and is responsible for picking out the keys (in DriverState) that
// matter to it.

// SessionContext lets a Driver query lightweight, time-varying facts about
// its owning session without taking a back-reference to core/. Implemented
// by Coordinator (via a per-session adapter) and injected into Drivers at
// construction time through Deps.Session.
//
// The "active" state lives exclusively in Coordinator.activeWindowID — the
// Driver pulls instead of caching, so there is no notification ordering
// problem to coordinate between push and poll paths.
type SessionContext interface {
	// Active reports whether the session's tmux window is currently
	// swapped into the main pane (0.0). Drivers use this to gate
	// expensive polling work (e.g. Tick refreshing transcript meta).
	Active() bool
	// ID returns the immutable session id this context belongs to.
	// Drivers cache this once at construction and use it for any
	// per-session resources they manage themselves (e.g. event log
	// file names) without taking a back-reference to core/.
	ID() string
}

// inactiveSessionContext is the zero value used when no SessionContext was
// injected. Always reports inactive — drivers that gate on Active() then
// behave as if they are not the focused session, which is the safe default.
type inactiveSessionContext struct{}

func (inactiveSessionContext) Active() bool { return false }
func (inactiveSessionContext) ID() string   { return "" }
