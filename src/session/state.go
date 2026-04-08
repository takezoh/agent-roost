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

// Session is the in-memory view of a roost-managed tmux window. It is built
// from tmux window user options (@roost_*) and is not persisted to disk —
// the tmux window itself owns the source of truth.
type Session struct {
	ID             string
	Project        string
	Command        string
	WindowID       string
	AgentSessionID string
	CreatedAt      time.Time
	Tags           []Tag

	State          State
	StateChangedAt time.Time
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
	WindowID       string
	ID             string
	Project        string
	Command        string
	CreatedAt      string
	Tags           string
	AgentSessionID string
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
