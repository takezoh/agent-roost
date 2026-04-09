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

	// Status / metadata readers
	Status() (StatusInfo, bool)
	Title() string
	LastPrompt() string
	Subjects() []string
	StatusLine() string
	Indicators() []string

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
