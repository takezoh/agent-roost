package session

import (
	"path/filepath"
	"time"
)

// Session is the in-memory view of a roost-managed tmux window. It holds only
// static metadata + the driver-managed DriverState bag — dynamic per-session
// status (running / waiting / pending / etc.) lives in state.Store and is
// owned by per-session driver Observer instances, never by Session itself.
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
	AgentPaneID string            `json:"-"`
	CreatedAt   time.Time         `json:"created_at"`
	Tags        []Tag             `json:"tags,omitempty"`
	DriverState map[string]string `json:"driver_state,omitempty"`
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
// avoiding an import cycle.
type RoostWindow struct {
	WindowID    string
	ID          string
	Project     string
	Command     string
	CreatedAt   string
	Tags        string
	AgentPaneID string
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
