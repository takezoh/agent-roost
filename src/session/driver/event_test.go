package driver

import "testing"

func TestAgentEvent_ToArgs_OmitsEmptyFields(t *testing.T) {
	ev := AgentEvent{
		Type: AgentEventSessionStart,
		DriverState: map[string]string{
			"session_id": "abc-123",
		},
	}
	args := ev.ToArgs()
	if args["type"] != "session-start" {
		t.Errorf("type = %q, want session-start", args["type"])
	}
	if args["drv_session_id"] != "abc-123" {
		t.Errorf("drv_session_id = %q", args["drv_session_id"])
	}
	for _, k := range []string{"state", "pane", "log", "drv_working_dir", "drv_transcript_path"} {
		if _, ok := args[k]; ok {
			t.Errorf("expected %q omitted, got %q", k, args[k])
		}
	}
}

func TestAgentEvent_RoundTrip(t *testing.T) {
	original := AgentEvent{
		Type:  AgentEventStateChange,
		State: "running",
		Pane:  "%5",
		Log:   "PreToolUse Bash ls",
		DriverState: map[string]string{
			"session_id":      "abc-123",
			"working_dir":     "/workspace/foo/.claude/worktrees/bar",
			"transcript_path": "/home/u/.claude/projects/-x/abc-123.jsonl",
		},
	}
	round := AgentEventFromArgs(original.ToArgs())
	if round.Type != original.Type ||
		round.State != original.State ||
		round.Pane != original.Pane ||
		round.Log != original.Log ||
		!stringMapsEqual(round.DriverState, original.DriverState) {
		t.Errorf("round trip mismatch:\n got = %+v\nwant = %+v", round, original)
	}
}

func TestAgentEventFromArgs_UnknownType(t *testing.T) {
	ev := AgentEventFromArgs(map[string]string{
		"type":           "weird",
		"drv_session_id": "abc",
	})
	if ev.Type != AgentEventType("weird") {
		t.Errorf("Type = %q", ev.Type)
	}
	if ev.DriverState["session_id"] != "abc" {
		t.Errorf("DriverState[session_id] = %q", ev.DriverState["session_id"])
	}
}

func TestAgentEvent_DriverStateOmittedWhenEmpty(t *testing.T) {
	ev := AgentEvent{Type: AgentEventStateChange, State: "running"}
	args := ev.ToArgs()
	for k := range args {
		if len(k) > 4 && k[:4] == "drv_" {
			t.Errorf("expected no drv_ keys, got %q", k)
		}
	}
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
