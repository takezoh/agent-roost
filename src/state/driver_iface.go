package state

import (
	"encoding/json"
	"strings"
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
// currently shown in pane 0.0 — drivers use it to gate expensive work
// that only matters when the user is looking. PaneTarget is the tmux
// pane id (e.g. "%5") for capture-pane polling.
// N and Seq are used for bucketing: drivers gate periodic work (e.g.
// capture-pane) to ticks where (N+Seq)%interval==0, so sessions are
// spread across different ticks rather than all firing simultaneously.
type DEvTick struct {
	Now        time.Time
	Active     bool
	Project    string
	PaneTarget string
	N          uint64 // monotonic tick counter from EvTick.N
	Seq        uint64 // position of this session in sorted order (0-indexed)
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

// ViewProvider is an optional capability for drivers that provide a
// custom TUI view.
type ViewProvider interface {
	// View is a pure getter for the current TUI payload. Same View
	// that Step returns, but callable without an event — used by the
	// runtime when serializing SessionInfo for broadcasts and when
	// flushing the active session's status line to tmux.
	View(s DriverState) View
}

// Persister is an optional capability for drivers that support
// session persistence across daemon restarts.
type Persister interface {
	// Persist serializes the driver state into a JSON-friendly map for
	// sessions.json. The reverse is Restore.
	Persist(s DriverState) map[string]string

	// Restore deserializes the persisted bag back into a DriverState.
	// Empty / unknown bags must return a usable zero-state value.
	Restore(bag map[string]string, now time.Time) DriverState
}

type LaunchMode int

const (
	LaunchModeCreate LaunchMode = iota
	LaunchModeColdStart
	LaunchModeWarmStart
)

type WorktreeOption struct {
	Enabled bool `json:"enabled,omitempty"`
}

type LaunchOptions struct {
	Worktree     WorktreeOption `json:"worktree,omitempty"`
	InitialInput []byte         `json:"initial_input,omitempty"`
}

type LaunchPlan struct {
	Command  string
	StartDir string
	Options  LaunchOptions
	Stdin    []byte // content piped into the spawned command; nil = no stdin
}

type LaunchPreparer interface {
	PrepareLaunch(s DriverState, mode LaunchMode, project, baseCommand string, options LaunchOptions) (LaunchPlan, error)
}

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

	ViewProvider
	Persister
	LaunchPreparer
}

// CreateLaunch is the fully resolved process launch information for a
// newly created session: command string plus tmux start directory.
type CreateLaunch struct {
	Command  string
	StartDir string
	Options  LaunchOptions
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
	PrepareCreate(s DriverState, sessionID SessionID, project, command string, options LaunchOptions) (DriverState, CreatePlan, error)
	CompleteCreate(s DriverState, command string, options LaunchOptions, result any, err error) (DriverState, CreateLaunch, error)
}

// ManagedWorktreeProvider is an optional driver extension for exposing
// a roost-managed worktree path that should be cleaned up on launch
// failure.
type ManagedWorktreeProvider interface {
	ManagedWorktreePath(s DriverState) string
}

// WarmStartRecoverer is an optional driver extension for restoring
// driver-owned runtime state after a warm start. Drivers use this to
// re-install watches and resume async parsing from already-restored
// DriverState without the runtime inspecting driver-specific fields.
type WarmStartRecoverer interface {
	WarmStartRecover(s DriverState, now time.Time) (DriverState, []Effect)
}

// StartDirAware is an optional driver extension that lets the state
// layer read and write the session's working directory without
// inspecting driver-specific concrete types. Used by reducePushDriver
// to inherit the root frame's directory into a new child frame.
type StartDirAware interface {
	// StartDir returns the working directory stored in the given DriverState.
	StartDir(s DriverState) string
	// WithStartDir returns a copy of s with the working directory set to dir.
	WithStartDir(s DriverState, dir string) DriverState
}

// driver registry. set once at init time by each driver impl package.
var (
	driverRegistry  = make(map[string]Driver)
	fallbackFactory func(command string) Driver
)

// Register adds a Driver to the registry under its Name(). Called from
// init() in each driver impl package. Panics on duplicate names so the
// daemon fails fast at startup if two impls collide.
func Register(d Driver) {
	if _, exists := driverRegistry[d.Name()]; exists {
		panic("state: duplicate driver registration: " + d.Name())
	}
	driverRegistry[d.Name()] = d
}

// RegisterFallbackFactory installs a factory used by GetDriver when
// the command does not match any registered driver. The factory
// receives the raw command string and returns a fresh Driver instance.
func RegisterFallbackFactory(factory func(command string) Driver) {
	fallbackFactory = factory
}

// GetRegistry returns the current driver registry. Used for testing.
func GetRegistry() map[string]Driver {
	return driverRegistry
}

// ClearRegistry clears the driver registry and fallback factory. Used for testing.
func ClearRegistry() {
	driverRegistry = map[string]Driver{}
	fallbackFactory = nil
}

// FirstToken extracts the first whitespace-delimited word from a command string.
func FirstToken(command string) string {
	if idx := strings.IndexAny(command, " \t"); idx != -1 {
		return command[:idx]
	}
	return command
}

// GetDriver returns the Driver for the given session command. It first
// tries to resolve the command's first token against the registry. If
// no registered driver matches and a fallback factory is installed, the
// factory is called to build a fresh driver. Otherwise the "" fallback
// driver is returned as the last resort.
func GetDriver(command string) Driver {
	name := FirstToken(command)
	if d, ok := driverRegistry[name]; ok {
		return d
	}
	if fallbackFactory != nil {
		return fallbackFactory(command)
	}
	return driverRegistry[""]
}
