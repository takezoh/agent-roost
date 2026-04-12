package state

import (
	"encoding/json"
	"time"
)

// DriverState is the per-session, per-driver private state value. Each
// driver impl defines its own concrete type (e.g. driver.ClaudeState,
// driver.GenericState) by embedding DriverStateBase. DriverState values
// are stored inside Session.Driver and round-tripped through reduce.go
// without inspection.
//
// The marker method is unexported to seal the interface to types that
// embed DriverStateBase, so adding a new driver state requires going
// through the explicit embed (visible in code review) rather than
// satisfying the interface accidentally.
type DriverState interface {
	driverStateMarker()
}

// DriverStateBase is the embed-only marker that promotes a struct into
// a valid DriverState. Driver impls embed this as their first field:
//
//	type GenericState struct {
//	    state.DriverStateBase
//	    Status state.Status
//	    ...
//	}
type DriverStateBase struct{}

func (DriverStateBase) driverStateMarker() {}

// DriverEvent is the closed sum type the reducer hands to a Driver's
// Step method. Concrete cases below cover every reason a driver state
// might transition.
type DriverEvent interface {
	isDriverEvent()
}

// DEvHook is a hook event from the agent via `roost event <eventType>`.
// Payload is the raw JSON from stdin.
type DEvHook struct {
	Event          string
	Timestamp      time.Time
	RoostSessionID string
	Payload        json.RawMessage
}

func (DEvHook) isDriverEvent() {}

// DEvTick is the periodic tick. Active reflects whether this session is
// currently swapped into pane 0.0 — drivers use it to gate expensive
// work that only matters when the user is looking. WindowTarget is the
// tmux window index (e.g. "1", "2") for capture-pane polling.
type DEvTick struct {
	Now          time.Time
	Active       bool
	Project      string
	WindowTarget string
}

func (DEvTick) isDriverEvent() {}

// DEvJobResult delivers an async worker pool result back to the driver
// that requested it. Result is typed by the worker (the driver dispatches
// on its concrete type) and Err is non-nil when the job failed. Now is
// the time the result is being applied; drivers use it to stamp
// StatusInfo / Activity rather than reading wall-clock from inside Step.
type DEvJobResult struct {
	Result any
	Err    error
	Now    time.Time
}

func (DEvJobResult) isDriverEvent() {}

// DEvFileChanged is fired by the runtime fsnotify watcher when a
// session's watched file changes on disk. Drivers typically respond
// by emitting EffStartJob{JobTranscriptParse}.
type DEvFileChanged struct {
	Path string
}

func (DEvFileChanged) isDriverEvent() {}

// Driver is the interface every per-driver-type plugin implements. Each
// impl is a stateless value type registered once at init time; the
// per-session state lives in DriverState values returned by NewState.
type Driver interface {
	// Name is the registry key (e.g. "mydriver").
	Name() string

	// DisplayName is the human-readable label shown in card / palette.
	DisplayName() string

	// NewState constructs a fresh DriverState for a brand-new session.
	// Initial status, idle counters, etc. live here.
	NewState(now time.Time) DriverState

	// Step is the per-driver reducer. It must be a pure function: no
	// I/O, no goroutines, no globals (other than the registry). All
	// side effects are returned as []Effect for the runtime to execute.
	Step(prev DriverState, ev DriverEvent) (DriverState, []Effect, View)

	// Status returns the current driver status without building the
	// full View. Used by the tick reducer to skip idle/stopped sessions.
	Status(s DriverState) Status

	// View is a pure getter for the current TUI payload. Same View
	// that Step returns, but callable without an event — used by the
	// runtime when serializing SessionInfo for broadcasts and when
	// flushing the active session's status line to tmux.
	View(s DriverState) View

	// SpawnCommand returns the shell command for (re)starting the agent
	// process during cold-boot recovery. Drivers that support resume
	// (e.g. mydriver --resume <id>) augment the base command using their
	// own keys recovered from the persisted state.
	SpawnCommand(s DriverState, baseCommand string) string

	// Persist serializes the driver state into a JSON-friendly map for
	// sessions.json. The reverse is Restore.
	Persist(s DriverState) map[string]string

	// Restore deserializes the persisted bag back into a DriverState.
	// Empty / unknown bags must return a usable zero-state value.
	Restore(bag map[string]string, now time.Time) DriverState
}

// CreateLaunch is the fully resolved process launch information for a
// newly created session: command string plus tmux start directory.
type CreateLaunch struct {
	Command  string
	StartDir string
}

// CreatePlan is the driver-owned create-session plan. Drivers that do
// not need any setup simply return Launch with SetupJob nil.
type CreatePlan struct {
	Launch   CreateLaunch
	SetupJob JobInput
}

// CreateSessionPlanner is an optional driver extension for commands
// that need to transform or prepare their start environment during
// create-session before tmux spawn happens.
type CreateSessionPlanner interface {
	PrepareCreate(s DriverState, sessionID SessionID, project, command string) (DriverState, CreatePlan, error)
	CompleteCreate(s DriverState, command string, result any, err error) (DriverState, CreateLaunch, error)
}

// ManagedWorktreeProvider is an optional driver extension for exposing
// a roost-managed worktree path that should be cleaned up on launch
// failure.
type ManagedWorktreeProvider interface {
	ManagedWorktreePath(s DriverState) string
}

// driver registry. set once at init time by each driver impl package.
var registry = map[string]Driver{}

// Register adds a Driver to the registry under its Name(). Called from
// init() in each driver impl package. Panics on duplicate names so the
// daemon fails fast at startup if two impls collide.
func Register(d Driver) {
	name := d.Name()
	if _, exists := registry[name]; exists {
		panic("state: duplicate driver registration: " + name)
	}
	registry[name] = d
}

// GetDriver returns the Driver for the given session command. Resolves
// the command string (which may include flags) down to a driver name
// via commandToDriverName. The fallback driver is registered under the
// empty name; callers can rely on a non-nil return as long as a fallback
// has been registered.
func GetDriver(command string) Driver {
	name := commandToDriverName(command)
	if d, ok := registry[name]; ok {
		return d
	}
	return registry[""]
}

// commandToDriverName extracts the registry key from a session command
// string. Currently a literal first-token match — "mydriver --flag X"
// → "mydriver". Anything not registered maps to "" so the fallback
// driver picks it up.
func commandToDriverName(command string) string {
	for i := 0; i < len(command); i++ {
		if command[i] == ' ' || command[i] == '\t' {
			command = command[:i]
			break
		}
	}
	if _, ok := registry[command]; ok {
		return command
	}
	return ""
}
