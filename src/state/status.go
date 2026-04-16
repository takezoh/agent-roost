package state

import (
	"encoding/json"
	"time"
)

// Status is the user-facing operational state of an agent session. The
// set is closed: drivers must report one of these or no status at all.
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

// SymbolKey returns a semantic key for the status icon, resolved to a
// glyph by the tui/glyphs package. Keeping glyph data out of state/
// preserves the dependency direction (tui → state, not state → tui).
func (s Status) SymbolKey() string {
	switch s {
	case StatusRunning:
		return "status.running"
	case StatusWaiting:
		return "status.waiting"
	case StatusIdle:
		return "status.idle"
	case StatusStopped:
		return "status.stopped"
	case StatusPending:
		return "status.pending"
	default:
		return ""
	}
}

// ParseStatus is the inverse of Status.String(). Returns ok=false on
// unknown / empty input — callers must decide what to do rather than
// relying on a silent fallback.
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
		return &json.UnmarshalTypeError{Value: name}
	}
	*s = parsed
	return nil
}

// StatusInfo bundles a status with the time it was entered. Drivers
// produce this from Step's view payload via DriverState.
type StatusInfo struct {
	Status    Status
	ChangedAt time.Time
}
