package claude

import (
	"encoding/json"
	"path/filepath"
)

// HookEvent represents a Claude Code hook event received on stdin.
type HookEvent struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	HookEventName  string `json:"hook_event_name"`
	Source         string `json:"source"`
}

// ParseHookEvent parses a Claude Code hook event from JSON bytes.
func ParseHookEvent(data []byte) (HookEvent, error) {
	var e HookEvent
	err := json.Unmarshal(data, &e)
	return e, err
}

// TranscriptFile returns the filename portion of the transcript path.
func (e HookEvent) TranscriptFile() string {
	return filepath.Base(e.TranscriptPath)
}
