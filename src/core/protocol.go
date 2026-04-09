package core

import (
	"path/filepath"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

type Message struct {
	Type string `json:"type"`

	// Command fields (client → server)
	Command string            `json:"command,omitempty"`
	Args    map[string]string `json:"args,omitempty"`

	// Event fields (server → client)
	Event           string        `json:"event,omitempty"`
	Sessions        []SessionInfo `json:"sessions,omitempty"`
	Error           string        `json:"error,omitempty"`
	ActiveWindowID  string        `json:"active_window_id,omitempty"`
	SelectedProject string        `json:"selected_project,omitempty"`
	// IsPreview marks a sessions-changed event as triggered by Preview
	// (cursor hover) rather than Switch. The log pane uses this to
	// activate the INFO tab on preview only.
	IsPreview bool `json:"is_preview,omitempty"`
	// Pane is set on "pane-focused" events. Identifies which tmux pane
	// (e.g. "0.0" for the main pane) just gained focus.
	Pane string `json:"pane,omitempty"`
}

// SessionInfo is the wire form of one roost session shipped to the TUI.
// Generic fields (ID, Project, Command, WindowID, CreatedAt, State,
// StateChangedAt) are rendered by the TUI directly. View carries all
// driver-owned UI content (Card / LogTabs / InfoExtras / StatusLine).
type SessionInfo struct {
	ID             string             `json:"id"`
	Project        string             `json:"project"`
	Command        string             `json:"command"`
	WindowID       string             `json:"window_id"`
	CreatedAt      string             `json:"created_at"`
	State          driver.Status      `json:"state"`
	StateChangedAt string             `json:"state_changed_at,omitempty"`
	View           driver.SessionView `json:"view"`
}

func (si SessionInfo) DisplayCommand() string {
	if si.Command != "" {
		return si.Command
	}
	return "idle"
}

func (si SessionInfo) Name() string {
	return filepath.Base(si.Project)
}

func (si SessionInfo) CreatedAtTime() time.Time {
	t, _ := time.Parse(time.RFC3339, si.CreatedAt)
	return t
}

func (si SessionInfo) StateChangedAtTime() time.Time {
	if si.StateChangedAt == "" {
		return si.CreatedAtTime()
	}
	t, _ := time.Parse(time.RFC3339, si.StateChangedAt)
	return t
}

func NewCommand(cmd string, args map[string]string) Message {
	return Message{Type: "command", Command: cmd, Args: args}
}

func NewEvent(event string) Message {
	return Message{Type: "event", Event: event}
}

// BuildSessionInfos pulls static metadata from SessionService and dynamic
// state from each session's Driver. There is no resolution / fallback layer
// — fields the Driver doesn't expose via View() are simply absent.
func BuildSessionInfos(sessions []*session.Session, drivers *driver.DriverService) []SessionInfo {
	infos := make([]SessionInfo, len(sessions))
	for i, s := range sessions {
		info := SessionInfo{
			ID:        s.ID,
			Project:   s.Project,
			Command:   s.Command,
			WindowID:  s.WindowID,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
		}
		if drv, ok := drivers.Get(s.ID); ok {
			if st, has := drv.Status(); has {
				info.State = st.Status
				if !st.ChangedAt.IsZero() {
					info.StateChangedAt = st.ChangedAt.Format(time.RFC3339)
				}
			}
			info.View = drv.View()
		}
		infos[i] = info
	}
	return infos
}
