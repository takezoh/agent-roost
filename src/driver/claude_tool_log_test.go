package driver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// hookEventAny builds a DEvHook whose payload is marshalled from an
// arbitrary-type map, allowing bool/number/object values needed by
// tool-log tests (e.g. is_interrupt, tool_input).
func hookEventAny(eventName string, fields map[string]any, ts time.Time) state.DEvHook {
	fields["hook_event_name"] = eventName
	raw, _ := json.Marshal(fields)
	return state.DEvHook{
		Event:     eventName,
		Timestamp: ts,
		Payload:   json.RawMessage(raw),
	}
}

// makeClaudeWithSession returns a state ready for tool-log tests:
// SessionStart absorbed, cwd=/work, session_id=sid.
func makeClaudeWithSession(t *testing.T) (ClaudeDriver, ClaudeState, time.Time) {
	t.Helper()
	d, cs, now := newClaude(t)
	cs, _ = d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "sid",
		"cwd":             "/work",
		"hook_event_name": "SessionStart",
	}, now))
	return d, cs, now
}

// findToolLogAppend returns the first EffToolLogAppend in effects.
func findToolLogAppend(effs []state.Effect) (state.EffToolLogAppend, bool) {
	for _, e := range effs {
		if v, ok := e.(state.EffToolLogAppend); ok {
			return v, true
		}
	}
	return state.EffToolLogAppend{}, false
}

// decodeToolLogLine unmarshals a tool log JSONL line.
func decodeToolLogLine(t *testing.T, line string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("decodeToolLogLine: %v (input: %q)", err, line)
	}
	return m
}

// === Tool log hook tests ===

func TestToolLog_AutoExecute(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)

	// PreToolUse
	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":      "sid",
		"cwd":             "/work",
		"tool_name":       "Bash",
		"tool_use_id":     "id1",
		"permission_mode": "default",
		"tool_input":      map[string]any{"command": "ls"},
	}, t1))

	// No Notification — auto-execute
	cs, effs := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":      "sid",
		"cwd":             "/work",
		"tool_name":       "Bash",
		"tool_use_id":     "id1",
		"permission_mode": "default",
	}, t2))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	if eff.Project == "" {
		t.Error("Project must not be empty")
	}
	m := decodeToolLogLine(t, eff.Line)
	if got := m["kind"]; got != "auto" {
		t.Errorf("kind = %v, want auto", got)
	}
	if got := m["tool_name"]; got != "Bash" {
		t.Errorf("tool_name = %v, want Bash", got)
	}
	// Entry should be removed from PendingTools
	if len(cs.PendingTools) != 0 {
		t.Errorf("PendingTools should be empty after Post, got %d", len(cs.PendingTools))
	}
}

func TestToolLog_UserApproved(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)
	t3 := now.Add(3 * time.Second)

	// Pre
	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))

	// Permission prompt fires (user is asked)
	cs, _ = d.handleHook(cs, hookEvent("Notification", map[string]string{
		"session_id":        "sid",
		"hook_event_name":   "Notification",
		"notification_type": "permission_prompt",
	}, t2))

	// Post (user approved)
	_, effs := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t3))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	m := decodeToolLogLine(t, eff.Line)
	if got := m["kind"]; got != "approved" {
		t.Errorf("kind = %v, want approved", got)
	}
}

func TestToolLog_Denied(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)

	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))

	_, effs := d.handleHook(cs, hookEventAny("PostToolUseFailure", map[string]any{
		"session_id":   "sid",
		"cwd":          "/work",
		"tool_name":    "Bash",
		"tool_use_id":  "id1",
		"is_interrupt": true,
	}, t2))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	m := decodeToolLogLine(t, eff.Line)
	if got := m["kind"]; got != "denied" {
		t.Errorf("kind = %v, want denied", got)
	}
}

func TestToolLog_Failed(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)

	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))

	_, effs := d.handleHook(cs, hookEventAny("PostToolUseFailure", map[string]any{
		"session_id":   "sid",
		"cwd":          "/work",
		"tool_name":    "Bash",
		"tool_use_id":  "id1",
		"is_interrupt": false,
		"error":        "exit status 1",
	}, t2))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	m := decodeToolLogLine(t, eff.Line)
	if got := m["kind"]; got != "failed" {
		t.Errorf("kind = %v, want failed", got)
	}
	if got := m["error"]; got != "exit status 1" {
		t.Errorf("error = %v, want 'exit status 1'", got)
	}
}

func TestToolLog_ParallelOldestGetsPrompt(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)
	t3 := now.Add(3 * time.Second)
	t4 := now.Add(4 * time.Second)
	t5 := now.Add(5 * time.Second)

	// Pre for A (older)
	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "idA",
	}, t1))
	// Pre for B (newer)
	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Read",
		"tool_use_id": "idB",
	}, t2))

	// One permission_prompt fires — should mark A (oldest)
	cs, _ = d.handleHook(cs, hookEvent("Notification", map[string]string{
		"session_id":        "sid",
		"hook_event_name":   "Notification",
		"notification_type": "permission_prompt",
	}, t3))

	// Post A → approved
	cs, effsA := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "idA",
	}, t4))
	// Post B → auto
	_, effsB := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Read",
		"tool_use_id": "idB",
	}, t5))

	effA, okA := findToolLogAppend(effsA)
	effB, okB := findToolLogAppend(effsB)
	if !okA || !okB {
		t.Fatalf("expected both EffToolLogAppend, got A=%v B=%v", okA, okB)
	}
	mA := decodeToolLogLine(t, effA.Line)
	mB := decodeToolLogLine(t, effB.Line)
	if got := mA["kind"]; got != "approved" {
		t.Errorf("A kind = %v, want approved", got)
	}
	if got := mB["kind"]; got != "auto" {
		t.Errorf("B kind = %v, want auto", got)
	}
}

func TestToolLog_Orphan(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)

	// Post arrives without a preceding Pre (e.g. daemon restart)
	_, effs := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "orphan-id",
	}, now.Add(time.Second)))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend for orphan")
	}
	m := decodeToolLogLine(t, eff.Line)
	if got := m["kind"]; got != "orphan" {
		t.Errorf("kind = %v, want orphan", got)
	}
}

func TestToolLog_SessionStartClearsPending(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)

	// Pre recorded
	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))
	if len(cs.PendingTools) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(cs.PendingTools))
	}

	// SessionStart clears pending
	cs, _ = d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "sid",
		"cwd":             "/work",
		"hook_event_name": "SessionStart",
	}, t2))
	if len(cs.PendingTools) != 0 {
		t.Errorf("PendingTools should be nil after SessionStart, got %d", len(cs.PendingTools))
	}
}

func TestToolLog_ProjectKeyFromCwd(t *testing.T) {
	d, cs, now := newClaude(t)
	// StartDir is empty (no SessionStart absorbed)
	t1 := now.Add(time.Second)
	t2 := now.Add(2 * time.Second)

	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/my/project",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))

	_, effs := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/my/project",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t2))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	// Project slug should be derived from /my/project
	wantProject := projectDir("/my/project")
	if eff.Project != wantProject {
		t.Errorf("Project = %q, want %q", eff.Project, wantProject)
	}
}

func TestToolLog_DurationMs(t *testing.T) {
	d, cs, now := makeClaudeWithSession(t)
	t1 := now.Add(time.Second)
	t2 := now.Add(3 * time.Second) // 2000 ms after Pre

	cs, _ = d.handleHook(cs, hookEventAny("PreToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t1))
	_, effs := d.handleHook(cs, hookEventAny("PostToolUse", map[string]any{
		"session_id":  "sid",
		"cwd":         "/work",
		"tool_name":   "Bash",
		"tool_use_id": "id1",
	}, t2))

	eff, ok := findToolLogAppend(effs)
	if !ok {
		t.Fatal("expected EffToolLogAppend")
	}
	m := decodeToolLogLine(t, eff.Line)
	dur, _ := m["duration_ms"].(float64)
	if dur != 2000 {
		t.Errorf("duration_ms = %v, want 2000", dur)
	}
}

// === summariseToolInput tests ===

func TestSummariseToolInput_BashTruncates(t *testing.T) {
	long := strings.Repeat("x", 300)
	out := summariseToolInput("Bash", map[string]any{"command": long})
	cmd, _ := out["command"].(string)
	// 200 runes + ellipsis
	if len([]rune(cmd)) > 201 {
		t.Errorf("command not truncated: len=%d", len(cmd))
	}
	if !strings.HasSuffix(cmd, "…") {
		t.Error("expected ellipsis suffix")
	}
}

func TestSummariseToolInput_ReadPicksFilePath(t *testing.T) {
	out := summariseToolInput("Read", map[string]any{
		"file_path": "/foo/bar.go",
		"limit":     float64(100),
	})
	if _, ok := out["file_path"]; !ok {
		t.Error("expected file_path in output")
	}
	if _, ok := out["limit"]; ok {
		t.Error("limit should not be in output")
	}
}

func TestSummariseToolInput_OtherTool(t *testing.T) {
	out := summariseToolInput("WebFetch", map[string]any{
		"url":    "https://example.com",
		"prompt": "describe",
	})
	if _, ok := out["url"]; !ok {
		t.Error("expected url in output")
	}
	if _, ok := out["prompt"]; !ok {
		t.Error("expected prompt in output")
	}
}

// === buildToolLogLine tests ===

func TestBuildToolLogLine_RoundtripJSON(t *testing.T) {
	now := time.Date(2026, 4, 16, 12, 0, 0, 0, time.UTC)
	entry := toolLogEntry{
		TS:             now,
		RoostSessionID: "rsid",
		ToolName:       "Bash",
		Kind:           "approved",
		PermissionMode: "default",
		DurationMs:     500,
		ToolInput:      map[string]any{"command": "ls"},
	}
	line := buildToolLogLine(entry)

	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("line is not valid JSON: %v", err)
	}
	if m["kind"] != "approved" {
		t.Errorf("kind = %v", m["kind"])
	}
	if m["tool_name"] != "Bash" {
		t.Errorf("tool_name = %v", m["tool_name"])
	}
	if m["roost_session_id"] != "rsid" {
		t.Errorf("roost_session_id = %v", m["roost_session_id"])
	}
	if m["duration_ms"] != float64(500) {
		t.Errorf("duration_ms = %v", m["duration_ms"])
	}
	if strings.Contains(line, "\n") {
		t.Error("line must not contain newline")
	}
}
