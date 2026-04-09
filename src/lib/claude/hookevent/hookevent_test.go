package hookevent

import "testing"

func TestParseHookEvent(t *testing.T) {
	input := `{"session_id":"abc-123","transcript_path":"/home/user/.claude/projects/-workspace-myproject/abc-123.jsonl","cwd":"/workspace/myproject/.claude/worktrees/foo","hook_event_name":"SessionStart","source":"startup"}`
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
	if event.Cwd != "/workspace/myproject/.claude/worktrees/foo" {
		t.Errorf("Cwd = %q", event.Cwd)
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

func TestFormatLog(t *testing.T) {
	tests := []struct {
		event HookEvent
		want  string
	}{
		{HookEvent{HookEventName: "UserPromptSubmit"}, "UserPromptSubmit"},
		{HookEvent{HookEventName: "PreToolUse", ToolName: "Read", ToolInput: map[string]any{"file_path": "/src/main.go"}}, "PreToolUse Read /src/main.go"},
		{HookEvent{HookEventName: "PostToolUse", ToolName: "Bash", ToolInput: map[string]any{"command": "go test ./..."}}, "PostToolUse Bash go test ./..."},
		{HookEvent{HookEventName: "Notification", NotificationType: "permission_prompt"}, "Notification permission_prompt"},
		{HookEvent{HookEventName: "SessionStart", Source: "startup"}, "SessionStart startup"},
		{HookEvent{HookEventName: "Stop"}, "Stop"},
	}
	for _, tt := range tests {
		got := tt.event.FormatLog()
		if got != tt.want {
			t.Errorf("FormatLog() = %q, want %q", got, tt.want)
		}
	}
}

func TestDeriveState(t *testing.T) {
	tests := []struct {
		event string
		ntype string
		want  string
	}{
		{"UserPromptSubmit", "", "running"},
		{"PreToolUse", "", "running"},
		{"PostToolUse", "", "running"},
		{"SubagentStart", "", "running"},
		{"Stop", "", "waiting"},
		{"StopFailure", "", "waiting"},
		{"SessionEnd", "", "stopped"},
		{"Notification", "permission_prompt", "pending"},
		{"Notification", "idle_prompt", "waiting"},
		{"Notification", "elicitation_dialog", "waiting"},
		{"Notification", "auth_success", ""},
		{"SessionStart", "", "idle"},
		{"PostCompact", "", ""},
	}
	for _, tt := range tests {
		e := HookEvent{HookEventName: tt.event, NotificationType: tt.ntype}
		got := e.DeriveState()
		if got != tt.want {
			t.Errorf("DeriveState(%s/%s) = %q, want %q", tt.event, tt.ntype, got, tt.want)
		}
	}
}
