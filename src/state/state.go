// Package state holds the pure functional core of roost. State is a plain
// data type, Reduce is a pure function, Event and Effect are closed sum
// types. No goroutines, no I/O, no globals (except the driver registry,
// which is set once at init time).
//
// The runtime package interprets effects and feeds events back into Reduce.
// All concurrency lives in runtime; state is single-threaded by construction.
package state

import "time"

// Identifier types. Distinct named types catch the common mix-up of "session
// id vs window id" at the type level instead of at runtime.
type (
	SessionID string
	WindowID  string
	ConnID    uint64
	JobID     uint64
)

// State is the entire roost domain state at one point in time. Reduce
// produces a new State value from an existing State + an Event; the
// runtime swaps its single in-memory copy each tick of the event loop.
//
// Maps are owned by the state and updated copy-on-write inside Reduce —
// callers must not mutate a State they did not produce.
type State struct {
	Sessions    map[SessionID]Session
	Active      WindowID
	Subscribers map[ConnID]Subscriber
	Jobs        map[JobID]JobMeta
	NextJobID   JobID
	NextConnID  ConnID
	Now         time.Time // last tick timestamp; deterministic in tests
	ShutdownReq bool
	Aliases        map[string]string // command alias expansion (e.g. "clw" → "claude --worktree")
	DefaultCommand string            // fallback when session command is empty
}

// Session is the static metadata + driver state of one roost session.
// All dynamic per-session data lives in Driver (a sum-typed value), which
// each driver impl returns from its Step method.
type Session struct {
	ID          SessionID
	Project     string
	Command     string
	WindowID    WindowID
	PaneID string
	CreatedAt   time.Time
	Driver      DriverState // sum type implemented by driver impls
}

// Subscriber tracks a connected IPC client that has opted into broadcasts.
// Filters is the set of event names the client wants to receive; an empty
// list means "all events".
type Subscriber struct {
	ConnID  ConnID
	Filters []string
}

// JobMeta is the in-flight worker bookkeeping for one async job. The
// runtime worker pool reports back via EvJobResult, which the reducer
// looks up here to find which session the result belongs to.
type JobMeta struct {
	SessionID SessionID
	StartedAt time.Time
}

// New returns an empty State suitable for a fresh daemon start. Maps
// are initialised so callers can write into them without nil checks.
func New() State {
	return State{
		Sessions:    map[SessionID]Session{},
		Subscribers: map[ConnID]Subscriber{},
		Jobs:        map[JobID]JobMeta{},
	}
}
