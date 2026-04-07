package claude

import "testing"

func TestParseHookEvent(t *testing.T) {
	input := `{"session_id":"abc-123","transcript_path":"/home/user/.claude/projects/-workspace-myproject/abc-123.jsonl","hook_event_name":"SessionStart","source":"startup"}`
	event, err := ParseHookEvent([]byte(input))
	if err != nil {
		t.Fatalf("ParseHookEvent: %v", err)
	}
	if event.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want %q", event.SessionID, "abc-123")
	}
	if event.TranscriptPath != "/home/user/.claude/projects/-workspace-myproject/abc-123.jsonl" {
		t.Errorf("TranscriptPath = %q", event.TranscriptPath)
	}
	if event.HookEventName != "SessionStart" {
		t.Errorf("HookEventName = %q, want %q", event.HookEventName, "SessionStart")
	}
	if event.Source != "startup" {
		t.Errorf("Source = %q, want %q", event.Source, "startup")
	}
}

func TestParseHookEvent_Invalid(t *testing.T) {
	_, err := ParseHookEvent([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
