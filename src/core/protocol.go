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
	SessionLogPath  string        `json:"session_log_path,omitempty"`
	EventLogPath    string        `json:"event_log_path,omitempty"`
	TranscriptPath  string        `json:"transcript_path,omitempty"`
	SelectedProject string        `json:"selected_project,omitempty"`
	// IsPreview marks a sessions-changed event as triggered by Preview
	// (cursor hover) rather than Switch. The log pane uses this to
	// activate the INFO tab on preview only.
	IsPreview bool `json:"is_preview,omitempty"`
}

type SessionInfo struct {
	ID             string        `json:"id"`
	Project        string        `json:"project"`
	Command        string        `json:"command"`
	WindowID       string        `json:"window_id"`
	CreatedAt      string        `json:"created_at"`
	State          driver.Status `json:"state"`
	StateChangedAt string        `json:"state_changed_at,omitempty"`
	Tags           []session.Tag `json:"tags,omitempty"`
	Title          string        `json:"title,omitempty"`
	LastPrompt     string        `json:"last_prompt,omitempty"`
	Subjects       []string      `json:"subjects,omitempty"`
	StatusLine     string        `json:"status_line,omitempty"`

	// Indicators are driver-built status chips (e.g. current tool,
	// subagent counts, error counts) shown next to the session card.
	// Each entry is a pre-formatted, driver-neutral string so the core
	// layer never has to know about Claude-specific concepts.
	Indicators []string `json:"indicators,omitempty"`
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
// — fields the Driver doesn't expose are simply absent in the output.
func BuildSessionInfos(sessions []*session.Session, drivers *driver.DriverService) []SessionInfo {
	infos := make([]SessionInfo, len(sessions))
	for i, s := range sessions {
		info := SessionInfo{
			ID:        s.ID,
			Project:   s.Project,
			Command:   s.Command,
			WindowID:  s.WindowID,
			CreatedAt: s.CreatedAt.Format(time.RFC3339),
			Tags:      s.Tags,
		}
		if drv, ok := drivers.Get(s.ID); ok {
			if st, has := drv.Status(); has {
				info.State = st.Status
				if !st.ChangedAt.IsZero() {
					info.StateChangedAt = st.ChangedAt.Format(time.RFC3339)
				}
			}
			info.Title = drv.Title()
			info.LastPrompt = drv.LastPrompt()
			info.Subjects = drv.Subjects()
			info.StatusLine = drv.StatusLine()
			info.Indicators = drv.Indicators()
		}
		infos[i] = info
	}
	return infos
}
