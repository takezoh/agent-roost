package session

import (
	"path/filepath"
	"time"
)

// Session is the in-memory view of a roost-managed tmux window. It holds
// only static metadata + the driver-managed PersistedState bag — dynamic
// per-session state (status / title / lastPrompt / insight / tags) lives
// in the Driver instance owned by DriverService, never on Session itself.
//
// PersistedState is the opaque key/value bag the driver round-trips through
// tmux user options + sessions.json. SessionService never reads or writes
// individual keys — it serializes/deserializes the bag as a whole.
type Session struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Command  string `json:"command"`
	WindowID string `json:"window_id"`
	// AgentPaneID is the tmux pane id of the agent process (e.g. "%5"),
	// stable across swap-pane. Persisted to tmux user options so the
	// reaper can identify dead panes by id, but not to JSON because
	// tmux server restart reissues every pane id and the cold-boot path
	// re-queries each fresh window.
	AgentPaneID    string            `json:"-"`
	CreatedAt      time.Time         `json:"created_at"`
	PersistedState map[string]string `json:"persisted_state,omitempty"`
}

// RoostWindow is a raw snapshot of a roost-managed tmux window's user
// options. All fields are still in their tmux string form; SessionService
// decodes them into Session values. Defined in the session package (not
// tmux) so that SessionService can declare its TmuxClient interface
// without importing tmux, avoiding an import cycle.
type RoostWindow struct {
	WindowID       string
	ID             string
	Project        string
	Command        string
	CreatedAt      string
	AgentPaneID    string
	PersistedState string // JSON-encoded map[string]string
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
