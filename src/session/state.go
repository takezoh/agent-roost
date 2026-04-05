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
	default:
		return "?"
	}
}

type Session struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Command   string    `json:"command"`
	WindowID  string    `json:"window_id"`
	CreatedAt time.Time `json:"created_at"`

	State   State         `json:"-"`
	Cost    string        `json:"-"`
	Elapsed time.Duration `json:"-"`
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
