package session

import (
	"path/filepath"
	"time"
)

type State int

const (
	StateRunning State = iota
	StateWaiting
	StateIdle
	StateStopped
	StatePending // waiting for tool permission approval
)


func (s State) String() string {
	switch s {
	case StateRunning:
		return "running"
	case StateWaiting:
		return "waiting"
	case StateIdle:
		return "idle"
	case StateStopped:
		return "stopped"
	case StatePending:
		return "pending"
	default:
		return "unknown"
	}
}

func (s State) Symbol() string {
	switch s {
	case StateRunning:
		return "●"
	case StateWaiting:
		return "◆"
	case StateIdle:
		return "○"
	case StateStopped:
		return "■"
	case StatePending:
		return "◇"
	default:
		return "?"
	}
}

// Session is the in-memory view of a roost-managed tmux window. The runtime
// truth lives in tmux window user options (@roost_*); the JSON tags exist so
// the same struct can be serialized into sessions.json as a cold-boot snapshot
// (tmux user options are wiped when the tmux server dies, e.g. on PC reboot).
//
// Driver-specific persistent data lives entirely inside DriverState — a
// map[string]string whose keys are defined by the driver assigned to Command.
// core treats DriverState as opaque and only the driver knows how to interpret
// the keys. Adding a Codex-specific field requires no change in this file.
type Session struct {
	ID          string            `json:"id"`
	Project     string            `json:"project"`
	Command     string            `json:"command"`
	WindowID    string            `json:"window_id"`
	CreatedAt   time.Time         `json:"created_at"`
	Tags        []Tag             `json:"tags,omitempty"`
	DriverState map[string]string `json:"driver_state,omitempty"`

	State          State     `json:"-"`
	StateChangedAt time.Time `json:"-"`
}

type Tag struct {
	Text       string `json:"text"`
	Foreground string `json:"fg,omitempty"`
	Background string `json:"bg,omitempty"`
}

// RoostWindow is a raw snapshot of a roost-managed tmux window's user options.
// All fields are still in their tmux string form; Manager decodes them into
// Session values. Defined in the session package (not tmux) so that
// session.Manager can declare its TmuxClient interface without importing tmux,
// avoiding an import cycle (tmux already imports session for State).
type RoostWindow struct {
	WindowID    string
	ID          string
	Project     string
	Command     string
	CreatedAt   string
	Tags        string
	DriverState string // JSON-encoded map[string]string
}


func (s *Session) Name() string {
	return filepath.Base(s.Project)
}

func (s *Session) DisplayCommand() string {
	if s.Command != "" {
		return s.Command
	}
	return "idle"
}
