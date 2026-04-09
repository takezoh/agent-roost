// Package state owns the dynamic per-session status (running / waiting / etc.)
// and is the single source of truth for it. Manager / AgentStore / Observer
// implementations never store status separately — they all read and write
// through state.Store.
package state

import (
	"encoding/json"
	"time"
)

// Status is the user-facing operational status of an agent session.
type Status int

const (
	StatusRunning Status = iota
	StatusWaiting
	StatusIdle
	StatusStopped
	StatusPending // waiting for tool permission approval
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusWaiting:
		return "waiting"
	case StatusIdle:
		return "idle"
	case StatusStopped:
		return "stopped"
	case StatusPending:
		return "pending"
	default:
		return "unknown"
	}
}

func (s Status) Symbol() string {
	switch s {
	case StatusRunning:
		return "●"
	case StatusWaiting:
		return "◆"
	case StatusIdle:
		return "○"
	case StatusStopped:
		return "■"
	case StatusPending:
		return "◇"
	default:
		return "?"
	}
}

// ParseStatus turns the string returned by Status.String() back into the enum.
// Returns ok=false on unknown / empty input — callers must decide what to do
// rather than relying on a silent fallback.
func ParseStatus(name string) (Status, bool) {
	switch name {
	case "running":
		return StatusRunning, true
	case "waiting":
		return StatusWaiting, true
	case "idle":
		return StatusIdle, true
	case "stopped":
		return StatusStopped, true
	case "pending":
		return StatusPending, true
	default:
		return 0, false
	}
}

// MarshalJSON / UnmarshalJSON keep snapshots human-readable and decoupled
// from the enum's iota order.
func (s Status) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *Status) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	parsed, ok := ParseStatus(name)
	if !ok {
		return &json.UnmarshalTypeError{Value: name, Type: nil}
	}
	*s = parsed
	return nil
}

// Info is the dynamic status of one session.
type Info struct {
	Status    Status
	ChangedAt time.Time
}
