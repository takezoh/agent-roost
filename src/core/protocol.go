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
	Event    string                    `json:"event,omitempty"`
	Sessions []SessionInfo             `json:"sessions,omitempty"`
	States   map[string]session.State  `json:"states,omitempty"`
	Error          string                    `json:"error,omitempty"`
	ActiveWindowID  string                    `json:"active_window_id,omitempty"`
	SessionLogPath  string                    `json:"session_log_path,omitempty"`
	EventLogPath    string                    `json:"event_log_path,omitempty"`
	TranscriptPath  string                    `json:"transcript_path,omitempty"`
	SelectedProject string                    `json:"selected_project,omitempty"`
}

type SessionInfo struct {
	ID        string        `json:"id"`
	Project   string        `json:"project"`
	Command   string        `json:"command"`
	WindowID  string        `json:"window_id"`
	CreatedAt string        `json:"created_at"`
	State          session.State `json:"state"`
	StateChangedAt string        `json:"state_changed_at,omitempty"`
	Tags           []session.Tag  `json:"tags,omitempty"`
	Title      string        `json:"title,omitempty"`
	LastPrompt string        `json:"last_prompt,omitempty"`
	Subjects   []string      `json:"subjects,omitempty"`
	StatusLine string        `json:"status_line,omitempty"`
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

// BuildSessionInfos merges tmux sessions with agent session data.
func BuildSessionInfos(sessions []*session.Session, store *driver.AgentStore) []SessionInfo {
	infos := make([]SessionInfo, len(sessions))
	for i, s := range sessions {
		agent := store.GetByWindow(s.WindowID)
		info := SessionInfo{
			ID:        s.ID,
			Project:   s.Project,
			Command:   s.Command,
			WindowID:  s.WindowID,
			CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			State:     ResolveAgentState(s.Command, s.State, agent),
			Tags:      s.Tags,
		}
		if !s.StateChangedAt.IsZero() {
			info.StateChangedAt = s.StateChangedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		if agent != nil {
			info.Title = agent.Title
			info.LastPrompt = agent.LastPrompt
			info.Subjects = agent.Subjects
			info.StatusLine = agent.StatusLine
		}
		infos[i] = info
	}
	return infos
}
