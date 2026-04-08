package driver

import "testing"

func TestAgentEvent_ToArgs_OmitsEmptyFields(t *testing.T) {
	ev := AgentEvent{
		Type:           AgentEventSessionStart,
		AgentSessionID: "abc-123",
	}
	args := ev.ToArgs()
	if args["type"] != "session-start" {
		t.Errorf("type = %q, want session-start", args["type"])
	}
	if args["session_id"] != "abc-123" {
		t.Errorf("session_id = %q", args["session_id"])
	}
	for _, k := range []string{"working_dir", "transcript_path", "state", "pane", "log"} {
		if _, ok := args[k]; ok {
			t.Errorf("expected %q omitted, got %q", k, args[k])
		}
	}
}

func TestAgentEvent_RoundTrip(t *testing.T) {
	original := AgentEvent{
		Type:           AgentEventStateChange,
		AgentSessionID: "abc-123",
		WorkingDir:     "/workspace/foo/.claude/worktrees/bar",
		TranscriptPath: "/home/u/.claude/projects/-x/abc-123.jsonl",
		State:          "running",
		Pane:           "%5",
		Log:            "PreToolUse Bash ls",
	}
	round := AgentEventFromArgs(original.ToArgs())
	if round != original {
		t.Errorf("round trip mismatch:\n got = %+v\nwant = %+v", round, original)
	}
}

func TestAgentEventFromArgs_UnknownType(t *testing.T) {
	ev := AgentEventFromArgs(map[string]string{
		"type":       "weird",
		"session_id": "abc",
	})
	if ev.Type != AgentEventType("weird") {
		t.Errorf("Type = %q", ev.Type)
	}
	if ev.AgentSessionID != "abc" {
		t.Errorf("AgentSessionID = %q", ev.AgentSessionID)
	}
}
