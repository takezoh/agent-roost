package proto

import (
	"time"

	"github.com/take/agent-roost/state"
)

// Response is the closed sum type of every successful reply the
// daemon sends back to a request. Errors are not Responses — they
// are encoded directly into Envelope.Error / Envelope.Status as
// ErrorBody, since they have a uniform shape.
type Response interface {
	isResponse()
}

// RespOK is the empty success response. Used by commands that have
// nothing to return except "operation accepted" (stop-session,
// detach, focus-pane, ...).
type RespOK struct{}

func (RespOK) isResponse() {}

// RespCreateSession is the response to create-session. The runtime
// fills it in after the tmux spawn callback completes.
type RespCreateSession struct {
	SessionID string `json:"session_id"`
	WindowID  string `json:"window_id"`
}

func (RespCreateSession) isResponse() {}

// RespSessions is the response to list-sessions and the body of
// EvtSessionsChanged. Carries the full session table + the active
// window id.
type RespSessions struct {
	Sessions       []SessionInfo `json:"sessions"`
	ActiveWindowID string        `json:"active_window_id,omitempty"`
}

func (RespSessions) isResponse() {}

// RespActiveWindow is the response to preview-session and
// switch-session. Just the new active window id (the broadcast that
// follows carries the full session list).
type RespActiveWindow struct {
	ActiveWindowID string `json:"active_window_id"`
}

func (RespActiveWindow) isResponse() {}

// SessionInfo is the per-session payload shipped on the wire. Mirrors
// state.Session + the driver's View output. Carried inside
// RespSessions and EvtSessionsChanged. State and StateChangedAt are
// duplicated from View.Status / View.StatusChangedAt for client-side
// convenience (the TUI renders status colors and elapsed time
// without unwrapping the View).
type SessionInfo struct {
	ID             string       `json:"id"`
	Project        string       `json:"project"`
	Command        string       `json:"command"`
	WindowID       string       `json:"window_id"`
	PaneID    string       `json:"pane_id,omitempty"`
	CreatedAt      string       `json:"created_at"`
	State          state.Status `json:"state,omitempty"`
	StateChangedAt string       `json:"state_changed_at,omitempty"`
	View           state.View   `json:"view"`
}

// Name returns the display name for the session (basename of project).
func (si SessionInfo) Name() string {
	return baseName(si.Project)
}

// DisplayCommand returns the command string or "idle" when empty.
func (si SessionInfo) DisplayCommand() string {
	if si.Command != "" {
		return si.Command
	}
	return "idle"
}

// CreatedAtTime parses the on-the-wire CreatedAt string.
func (si SessionInfo) CreatedAtTime() time.Time {
	t, _ := time.Parse(time.RFC3339, si.CreatedAt)
	return t
}

// StateChangedAtTime parses StateChangedAt, falling back to CreatedAt
// when the state has not been touched yet.
func (si SessionInfo) StateChangedAtTime() time.Time {
	if si.StateChangedAt == "" {
		return si.CreatedAtTime()
	}
	t, _ := time.Parse(time.RFC3339, si.StateChangedAt)
	return t
}

// baseName mirrors filepath.Base without importing filepath, so the
// proto package stays trim. Handles both "/" and OS-native separators.
func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
