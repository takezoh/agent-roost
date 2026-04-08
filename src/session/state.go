package session

import (
	"encoding/json"
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

// ParseState turns the string returned by State.String() back into the enum.
// Unknown values fall through to StateIdle so loading a snapshot written by
// a future version with new states still produces something sensible.
func ParseState(name string) State {
	switch name {
	case "running":
		return StateRunning
	case "waiting":
		return StateWaiting
	case "idle":
		return StateIdle
	case "stopped":
		return StateStopped
	case "pending":
		return StatePending
	default:
		return StateIdle
	}
}

// MarshalJSON serializes State as a stable string ("running", ...) so JSON
// snapshots stay human-readable and decoupled from the enum's iota order.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

func (s *State) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	*s = ParseState(name)
	return nil
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
	ID       string `json:"id"`
	Project  string `json:"project"`
	Command  string `json:"command"`
	WindowID string `json:"window_id"`
	// AgentPaneID is the tmux pane id of the agent process (e.g. "%5"),
	// stable across swap-pane. Persisted to tmux user options so the
	// reaper can identify dead panes by id, but not to JSON because
	// tmux server restart reissues every pane id and Recreate re-queries.
	AgentPaneID    string            `json:"-"`
	CreatedAt      time.Time         `json:"created_at"`
	Tags           []Tag             `json:"tags,omitempty"`
	DriverState    map[string]string `json:"driver_state,omitempty"`
	State          State             `json:"state"`
	StateChangedAt time.Time         `json:"state_changed_at,omitempty"`
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
	AgentPaneID    string
	DriverState    string // JSON-encoded map[string]string
	State          string // State.String() form
	StateChangedAt string // RFC3339
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
