package claude

import (
	"encoding/json"
)

// HookEvent represents a Claude Code hook event received on stdin.
// Field names mirror the Claude Code hook payload — this struct is the
// only place in roost that knows about Claude's wire format.
type HookEvent struct {
	SessionID        string         `json:"session_id"`
	TranscriptPath   string         `json:"transcript_path"`
	Cwd              string         `json:"cwd"`
	HookEventName    string         `json:"hook_event_name"`
	Source           string         `json:"source"`
	NotificationType string         `json:"notification_type"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
}

// FormatLog returns a human-readable log line for the event.
func (e HookEvent) FormatLog() string {
	s := e.HookEventName
	switch e.HookEventName {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		s += " " + e.ToolName
		if e.ToolName == "Bash" {
			if cmd, ok := e.ToolInput["command"].(string); ok {
				if len(cmd) > 80 {
					cmd = cmd[:77] + "..."
				}
				s += " " + cmd
			}
		} else if e.ToolName == "Read" || e.ToolName == "Write" || e.ToolName == "Edit" || e.ToolName == "Glob" {
			if fp, ok := e.ToolInput["file_path"].(string); ok {
				s += " " + fp
			} else if p, ok := e.ToolInput["pattern"].(string); ok {
				s += " " + p
			}
		}
	case "Notification":
		if e.NotificationType != "" {
			s += " " + e.NotificationType
		}
	case "SessionStart":
		if e.Source != "" {
			s += " " + e.Source
		}
	}
	return s
}

// ParseHookEvent parses a Claude Code hook event from JSON bytes.
func ParseHookEvent(data []byte) (HookEvent, error) {
	var e HookEvent
	err := json.Unmarshal(data, &e)
	return e, err
}

// DeriveState returns a state string based on the hook event type.
// Returns empty string if the event doesn't map to a state change.
func (e HookEvent) DeriveState() string {
	switch e.HookEventName {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "SubagentStart":
		return "running"
	case "Stop", "StopFailure":
		return "waiting"
	case "SessionEnd":
		return "stopped"
	case "SessionStart":
		// A SessionStart fires for fresh launch / --resume / /resume / /clear.
		// In every case the new session is freshly initialized and hasn't done
		// anything yet, so Idle is the right starting point. This also resets
		// the Stopped that the preceding SessionEnd wrote on /resume — without
		// it the resumed session would stick at Stopped until the user typed
		// something, which is wrong because the agent is fully alive.
		return "idle"
	case "Notification":
		switch e.NotificationType {
		case "permission_prompt":
			return "pending"
		case "idle_prompt":
			return "waiting"
		}
	}
	return ""
}
