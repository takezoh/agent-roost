package driver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/lib/claude/transcript"
	"github.com/takezoh/agent-roost/state"
)

const testHome = "/home/test"
const testEventLogDir = "/data/events"

// hookEvent builds a DEvHook from the given fields and now time.
func hookEvent(eventName string, fields map[string]string, ts time.Time) state.DEvHook {
	raw, _ := json.Marshal(fields)
	return state.DEvHook{
		Event:     eventName,
		Timestamp: ts,
		Payload:   json.RawMessage(raw),
	}
}

func newClaude(t *testing.T) (ClaudeDriver, ClaudeState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := d.NewState(now).(ClaudeState)
	return d, cs, now
}

func findEffect[T state.Effect](effs []state.Effect) (T, bool) {
	var zero T
	for _, e := range effs {
		if v, ok := e.(T); ok {
			return v, true
		}
	}
	return zero, false
}

// === Hook handling ===

func TestClaudeSessionStartAbsorbsIdentityAndWatches(t *testing.T) {
	d, cs, now := newClaude(t)
	next, effs := d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "claude-uuid",
		"cwd":             "/work",
		"transcript_path": "/tmp/x.jsonl",
		"hook_event_name": "SessionStart",
	}, now))
	if next.ClaudeSessionID != "claude-uuid" {
		t.Errorf("ClaudeSessionID = %q, want claude-uuid", next.ClaudeSessionID)
	}
	if next.WorkingDir != "/work" {
		t.Errorf("WorkingDir = %q, want /work", next.WorkingDir)
	}
	if next.TranscriptPath != "/tmp/x.jsonl" {
		t.Errorf("TranscriptPath = %q, want /tmp/x.jsonl", next.TranscriptPath)
	}
	if next.WatchedFile != "/tmp/x.jsonl" {
		t.Errorf("WatchedFile = %q, want /tmp/x.jsonl", next.WatchedFile)
	}
	if !next.TranscriptInFlight {
		t.Error("TranscriptInFlight should be true after SessionStart")
	}
	if _, ok := findEffect[state.EffWatchFile](effs); !ok {
		t.Error("expected EffWatchFile")
	}
	job, ok := findEffect[state.EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	if _, ok := job.Input.(TranscriptParseInput); !ok {
		t.Errorf("job input type = %T, want TranscriptParseInput", job.Input)
	}
	if _, ok := findEffect[state.EffEventLogAppend](effs); !ok {
		t.Error("expected EffEventLogAppend")
	}
	// Branch detection should be dispatched immediately on SessionStart.
	if !next.BranchInFlight {
		t.Error("BranchInFlight should be true after SessionStart with cwd")
	}
	if next.BranchTarget != "/work" {
		t.Errorf("BranchTarget = %q, want /work", next.BranchTarget)
	}
	foundBranch := false
	for _, e := range effs {
		if j, ok := e.(state.EffStartJob); ok {
			if _, ok := j.Input.(BranchDetectInput); ok {
				foundBranch = true
			}
		}
	}
	if !foundBranch {
		t.Error("expected BranchDetectInput job in SessionStart effects")
	}
}

func TestClaudeSessionStartSkipsBranchDetectWhenInFlight(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.BranchInFlight = true
	next, effs := d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/work",
		"hook_event_name": "SessionStart",
	}, now))
	if !next.BranchInFlight {
		t.Error("BranchInFlight should remain true")
	}
	for _, e := range effs {
		if j, ok := e.(state.EffStartJob); ok {
			if _, ok := j.Input.(BranchDetectInput); ok {
				t.Error("should not dispatch BranchDetect while in-flight")
			}
		}
	}
}

func TestClaudeSessionStartNoCwdSkipsBranchDetect(t *testing.T) {
	d, cs, now := newClaude(t)
	next, _ := d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"hook_event_name": "SessionStart",
	}, now))
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false when cwd is empty")
	}
}

func TestClaudeSessionStartAbsorbsRoostSessionID(t *testing.T) {
	d, cs, now := newClaude(t)
	ev := hookEvent("SessionStart", map[string]string{
		"session_id":      "claude-uuid",
		"hook_event_name": "SessionStart",
	}, now)
	ev.RoostSessionID = "roost-abc"
	next, _ := d.handleHook(cs, ev)
	if next.RoostSessionID != "roost-abc" {
		t.Errorf("RoostSessionID = %q, want roost-abc", next.RoostSessionID)
	}
}

func TestClaudeStateChangeStopSetsWaiting(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"
	next, effs := d.handleHook(cs, hookEvent("Stop", map[string]string{
		"session_id":      "uuid",
		"hook_event_name": "Stop",
	}, now.Add(time.Second)))
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (Stop → waiting)", next.Status)
	}
	if !next.StatusChangedAt.Equal(now.Add(time.Second)) {
		t.Errorf("StatusChangedAt not updated")
	}
	logEff, ok := findEffect[state.EffEventLogAppend](effs)
	if !ok {
		t.Fatal("expected EffEventLogAppend")
	}
	if logEff.Line != "Stop" {
		t.Errorf("log line = %q, want Stop", logEff.Line)
	}
	if !next.TranscriptInFlight {
		t.Error("TranscriptInFlight should be true after state-change")
	}
}

func TestClaudeUnknownHookEventIsNoop(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.Status = state.StatusWaiting
	next, _ := d.handleHook(cs, hookEvent("Notification", map[string]string{
		"session_id":        "uuid",
		"hook_event_name":   "Notification",
		"notification_type": "something_else",
	}, time.Time{}))
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (unchanged)", next.Status)
	}
}

func TestClaudeUserPromptSubmitTriggersHaiku(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleHook(cs, hookEvent("UserPromptSubmit", map[string]string{
		"session_id":      "uuid",
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "do the thing",
	}, now))
	if next.LastPrompt != "do the thing" {
		t.Errorf("LastPrompt = %q, want %q", next.LastPrompt, "do the thing")
	}
	if !next.SummaryInFlight {
		t.Error("SummaryInFlight should be true")
	}
	job, ok := findEffect[state.EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	in, ok := job.Input.(SummaryCommandInput)
	if !ok {
		t.Fatalf("job input type = %T, want SummaryCommandInput", job.Input)
	}
	if !strings.Contains(in.Prompt, "do the thing") {
		t.Errorf("haiku prompt should include user prompt: %q", in.Prompt)
	}
}

func TestClaudeUserPromptSubmitDedupesWhileInFlight(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.SummaryInFlight = true
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleHook(cs, hookEvent("UserPromptSubmit", map[string]string{
		"session_id":      "uuid",
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "another",
	}, now))
	if !next.SummaryInFlight {
		t.Error("SummaryInFlight should remain true")
	}
	if _, ok := findEffect[state.EffStartJob](effs); ok {
		t.Error("should not start a new job while summary is in flight")
	}
}

func TestClaudeUnknownHookIsNoop(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.ClaudeSessionID = "before"
	cs.LastPrompt = "before"
	next, effs := d.handleHook(cs, state.DEvHook{Event: "garbage"})
	if next.ClaudeSessionID != cs.ClaudeSessionID || next.LastPrompt != cs.LastPrompt {
		t.Error("unknown hook should leave state untouched")
	}
	if len(effs) != 0 {
		t.Errorf("unknown hook effs = %d, want 0", len(effs))
	}
}

// === Hook ordering ===

func TestClaudeHookDropsStaleEvent(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"

	// First event at bridge_ts=now+200ms
	ev1 := hookEvent("Stop", map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	ev1.Timestamp = now.Add(200 * time.Millisecond)
	next, _ := d.handleHook(cs, ev1)
	if !next.LastBridgeTS.Equal(now.Add(200 * time.Millisecond)) {
		t.Fatalf("LastBridgeTS = %v, want now+200ms", next.LastBridgeTS)
	}
	if next.Status != state.StatusWaiting {
		t.Fatalf("Status = %v, want Waiting", next.Status)
	}

	// Second event at bridge_ts=now+100ms (stale) — must be dropped
	ev2 := hookEvent("UserPromptSubmit", map[string]string{
		"session_id": "uuid", "hook_event_name": "UserPromptSubmit",
		"prompt": "stale",
	}, now)
	ev2.Timestamp = now.Add(100 * time.Millisecond)
	next2, effs := d.handleHook(next, ev2)
	if next2.Status != state.StatusWaiting {
		t.Errorf("stale event changed status to %v", next2.Status)
	}
	if next2.LastPrompt == "stale" {
		t.Error("stale event should not set LastPrompt")
	}
	if len(effs) != 0 {
		t.Errorf("stale event produced %d effects, want 0", len(effs))
	}
}

func TestClaudeHookAcceptsMissingBridgeTS(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.LastBridgeTS = now.Add(500 * time.Millisecond)

	// No bridge_ts (Timestamp=zero) — should be accepted for backward compat
	ev := hookEvent("Stop", map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, time.Time{})
	next, _ := d.handleHook(cs, ev)
	if next.Status != state.StatusWaiting {
		t.Errorf("missing bridge_ts should be accepted, got status %v", next.Status)
	}
	if !next.LastBridgeTS.Equal(now.Add(500 * time.Millisecond)) {
		t.Errorf("LastBridgeTS changed to %v, should stay now+500ms", next.LastBridgeTS)
	}
}

func TestClaudeSessionStartBypassesOrdering(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.LastBridgeTS = now.Add(9000 * time.Millisecond) // high watermark from previous session

	// SessionStart with a lower bridge_ts (e.g. clock skew after NTP)
	ev := hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/work",
		"transcript_path": "/tmp/x.jsonl",
		"hook_event_name": "SessionStart",
	}, now)
	ev.Timestamp = now.Add(100 * time.Millisecond)
	next, effs := d.handleHook(cs, ev)
	if next.Status != state.StatusIdle {
		t.Errorf("SessionStart should always be accepted, got status %v", next.Status)
	}
	if !next.LastBridgeTS.Equal(now.Add(100 * time.Millisecond)) {
		t.Errorf("LastBridgeTS = %v, want now+100ms (reset by SessionStart)", next.LastBridgeTS)
	}
	if len(effs) == 0 {
		t.Error("SessionStart should produce effects")
	}
}

func TestClaudeHookAdvancesLastBridgeTS(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"

	ev := hookEvent("Stop", map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	ev.Timestamp = now.Add(300 * time.Millisecond)
	next, _ := d.handleHook(cs, ev)
	if !next.LastBridgeTS.Equal(now.Add(300 * time.Millisecond)) {
		t.Errorf("LastBridgeTS = %v, want now+300ms", next.LastBridgeTS)
	}
}

func TestClaudeHookPreToolUseThenNotification(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"

	// PreToolUse arrives first — status becomes Running.
	ev1 := hookEvent("PreToolUse", map[string]string{
		"session_id": "uuid", "hook_event_name": "PreToolUse",
		"tool_name": "Bash",
	}, now)
	ev1.Timestamp = now.Add(100 * time.Millisecond)
	next, _ := d.handleHook(cs, ev1)
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want Running", next.Status)
	}

	// Notification (permission_prompt) arrives with a higher bridge_ts
	// — must NOT be dropped. Before the bridge_ts-before-stdin fix,
	// the Notification process could get a lower timestamp than the
	// PreToolUse process due to stdin read latency, causing it to be
	// dropped as stale and leaving the session stuck at Running.
	ev2 := hookEvent("Notification", map[string]string{
		"session_id": "uuid", "hook_event_name": "Notification",
		"notification_type": "permission_prompt",
	}, now)
	ev2.Timestamp = now.Add(150 * time.Millisecond)
	next2, _ := d.handleHook(next, ev2)
	if next2.Status != state.StatusPending {
		t.Errorf("Notification should advance to Pending, got %v", next2.Status)
	}
}

func TestClaudeHookSubagentEventsDoNotChangeStatus(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"

	// Set pending via permission_prompt.
	ev0 := hookEvent("Notification", map[string]string{
		"session_id": "uuid", "hook_event_name": "Notification",
		"notification_type": "permission_prompt",
	}, now)
	ev0.Timestamp = now.Add(100 * time.Millisecond)
	cs, _ = d.handleHook(cs, ev0)
	if cs.Status != state.StatusPending {
		t.Fatalf("Status = %v, want Pending", cs.Status)
	}

	tests := []struct {
		event    string
		toolName string
	}{
		{"SubagentStart", ""},
		{"PostToolUse", "Agent"},
		{"PreToolUse", "Agent"},
	}
	for _, tt := range tests {
		t.Run(tt.event+tt.toolName, func(t *testing.T) {
			fields := map[string]string{
				"session_id":      "uuid",
				"hook_event_name": tt.event,
			}
			if tt.toolName != "" {
				fields["tool_name"] = tt.toolName
			}
			ev := hookEvent(tt.event, fields, now)
			ev.Timestamp = now.Add(200 * time.Millisecond)
			next, effs := d.handleHook(cs, ev)
			if next.Status != state.StatusPending {
				t.Errorf("%s %s: Status = %v, want Pending", tt.event, tt.toolName, next.Status)
			}
			if _, ok := findEffect[state.EffEventLogAppend](effs); !ok {
				t.Errorf("%s %s: expected EffEventLogAppend", tt.event, tt.toolName)
			}
		})
	}
}

func TestClaudePendingTransitionsToRunningOnToolUse(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"
	tsOff := 100

	hook := func(fields map[string]string) (ClaudeState, []state.Effect) {
		tsOff++
		ev := hookEvent(fields["hook_event_name"], fields, now)
		ev.Timestamp = now.Add(time.Duration(tsOff) * time.Millisecond)
		next, effs := d.handleHook(cs, ev)
		cs = next
		return next, effs
	}

	// Enter pending via permission_prompt.
	hook(map[string]string{
		"session_id": "uuid", "hook_event_name": "Notification",
		"notification_type": "permission_prompt",
	})
	if cs.Status != state.StatusPending {
		t.Fatalf("Status = %v, want Pending", cs.Status)
	}

	// A subagent starts (should not change status).
	hook(map[string]string{
		"session_id": "uuid", "hook_event_name": "SubagentStart",
	})
	if cs.Status != state.StatusPending {
		t.Fatalf("Status = %v, want Pending after SubagentStart", cs.Status)
	}

	// PreToolUse after permission approval must transition to Running
	// even while subagents are active.
	next, _ := hook(map[string]string{
		"session_id": "uuid", "hook_event_name": "PreToolUse",
		"tool_name": "Bash",
	})
	if next.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running after PreToolUse", next.Status)
	}
}

// === Tick handling (branch detection) ===

func TestClaudeTickInactiveIdleDoesNothing(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	// Idle (default) + inactive → no effects
	_, effs := d.handleTick(cs, state.DEvTick{Now: now, Active: false})
	if len(effs) != 0 {
		t.Errorf("inactive idle tick effs = %d, want 0", len(effs))
	}
}

func TestClaudeTickActiveSchedulesBranchJob(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	next, effs := d.handleTick(cs, state.DEvTick{Now: now, Active: true})
	if !next.BranchInFlight {
		t.Error("BranchInFlight should be true")
	}
	if next.BranchTarget != "/work" {
		t.Errorf("BranchTarget = %q, want /work", next.BranchTarget)
	}
	job, ok := findEffect[state.EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	if _, ok := job.Input.(BranchDetectInput); !ok {
		t.Errorf("job input type = %T, want BranchDetectInput", job.Input)
	}
	in, ok := job.Input.(BranchDetectInput)
	if !ok || in.WorkingDir != "/work" {
		t.Errorf("input = %v, want {WorkingDir: /work}", job.Input)
	}
}

func TestClaudeTickFreshCacheSkips(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	cs.BranchTarget = "/work"
	cs.BranchAt = now // fresh
	_, effs := d.handleTick(cs, state.DEvTick{Now: now.Add(time.Second), Active: true})
	if len(effs) != 0 {
		t.Errorf("fresh cache effs = %d, want 0", len(effs))
	}
}

func TestClaudeTickStaleCacheRefreshes(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	cs.BranchTarget = "/work"
	cs.BranchAt = now.Add(-time.Hour)
	_, effs := d.handleTick(cs, state.DEvTick{Now: now, Active: true})
	if _, ok := findEffect[state.EffStartJob](effs); !ok {
		t.Error("stale cache should refresh")
	}
}

func TestClaudeTickInFlightSkips(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	cs.BranchInFlight = true
	_, effs := d.handleTick(cs, state.DEvTick{Now: now, Active: true})
	if len(effs) != 0 {
		t.Errorf("in-flight effs = %d, want 0", len(effs))
	}
}

func TestClaudeWarmStartRecoverReinstallsTranscriptWatch(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.WorkingDir = "/work"
	nextState, effs := d.WarmStartRecover(cs, now)
	next := nextState.(ClaudeState)
	wantPath := "/home/test/.claude/projects/-work/uuid.jsonl"
	if next.TranscriptPath != wantPath {
		t.Fatalf("TranscriptPath = %q, want %q", next.TranscriptPath, wantPath)
	}
	if next.WatchedFile != wantPath {
		t.Fatalf("WatchedFile = %q, want %q", next.WatchedFile, wantPath)
	}
	if !next.TranscriptInFlight {
		t.Fatal("TranscriptInFlight should be true")
	}
	if len(effs) != 2 {
		t.Fatalf("effects = %d, want 2", len(effs))
	}
	if _, ok := effs[0].(state.EffWatchFile); !ok {
		t.Fatalf("first effect = %T, want EffWatchFile", effs[0])
	}
	job, ok := effs[1].(state.EffStartJob)
	if !ok {
		t.Fatalf("second effect = %T, want EffStartJob", effs[1])
	}
	if _, ok := job.Input.(TranscriptParseInput); !ok {
		t.Fatalf("job input = %T, want TranscriptParseInput", job.Input)
	}
	if next.Status != cs.Status {
		t.Fatalf("Status = %v, want %v", next.Status, cs.Status)
	}
}

func TestClaudeWarmStartRecoverDedupesTranscriptParse(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.WorkingDir = "/work"
	cs.TranscriptInFlight = true
	nextState, effs := d.WarmStartRecover(cs, now)
	next := nextState.(ClaudeState)
	wantPath := "/home/test/.claude/projects/-work/uuid.jsonl"
	if next.WatchedFile != wantPath {
		t.Fatalf("WatchedFile = %q, want %q", next.WatchedFile, wantPath)
	}
	if len(effs) != 1 {
		t.Fatalf("effects = %d, want 1", len(effs))
	}
	if _, ok := effs[0].(state.EffWatchFile); !ok {
		t.Fatalf("effect = %T, want EffWatchFile", effs[0])
	}
}

// === Transcript change handling ===

func TestClaudeTranscriptChangedSchedulesParse(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptPath = "/tmp/x.jsonl"
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleTranscriptChanged(cs, state.DEvFileChanged{Path: "/tmp/x.jsonl"})
	if next.WatchedFile != "/tmp/x.jsonl" {
		t.Fatalf("WatchedFile = %q, want /tmp/x.jsonl", next.WatchedFile)
	}
	if !next.TranscriptInFlight {
		t.Error("TranscriptInFlight should be true")
	}
	if _, ok := findEffect[state.EffWatchFile](effs); !ok {
		t.Fatal("expected EffWatchFile")
	}
	job, ok := findEffect[state.EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob")
	}
	if _, ok := job.Input.(TranscriptParseInput); !ok {
		t.Errorf("job input type = %T, want TranscriptParseInput", job.Input)
	}
}

func TestClaudeTranscriptChangedDedupes(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptInFlight = true
	cs.TranscriptPath = "/tmp/x.jsonl"
	next, effs := d.handleTranscriptChanged(cs, state.DEvFileChanged{Path: "/tmp/x.jsonl"})
	if next.WatchedFile != "/tmp/x.jsonl" {
		t.Fatalf("WatchedFile = %q, want /tmp/x.jsonl", next.WatchedFile)
	}
	if len(effs) != 1 {
		t.Fatalf("dedup effs = %d, want 1", len(effs))
	}
	if _, ok := effs[0].(state.EffWatchFile); !ok {
		t.Fatalf("effect = %T, want EffWatchFile", effs[0])
	}
}

// === Job result handling ===

func TestClaudeTranscriptParseResultMergesFields(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptInFlight = true
	now := time.Now()
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now: now,
		Result: TranscriptParseResult{
			Title:       "Refactor X",
			LastPrompt:  "please refactor",
			StatusLine:  "claude-3.5",
			CurrentTool: "Edit",
			Subagents:   map[string]int{"Task": 2},
		},
	})
	if next.TranscriptInFlight {
		t.Error("TranscriptInFlight should be false after result")
	}
	if next.Title != "Refactor X" || next.LastPrompt != "please refactor" {
		t.Errorf("title/prompt not merged: %+v", next)
	}
	if next.StatusLine != "claude-3.5" {
		t.Errorf("StatusLine = %q", next.StatusLine)
	}
	if next.CurrentTool != "Edit" {
		t.Errorf("CurrentTool = %q", next.CurrentTool)
	}
	if next.SubagentCounts["Task"] != 2 {
		t.Errorf("Subagents not merged: %v", next.SubagentCounts)
	}
}

func TestClaudeTranscriptParseEmptyLastPromptDoesNotErase(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.LastPrompt = "seed from hook"
	cs.TranscriptInFlight = true
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Result: TranscriptParseResult{LastPrompt: ""},
	})
	if next.LastPrompt != "seed from hook" {
		t.Errorf("empty parse erased seed: %q", next.LastPrompt)
	}
}

func TestClaudeTranscriptParseErrorClearsFlag(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptInFlight = true
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Err:    errors.New("read error"),
		Result: TranscriptParseResult{},
	})
	if next.TranscriptInFlight {
		t.Error("error should still clear in-flight flag")
	}
}

func TestClaudeHaikuResultMerges(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.SummaryInFlight = true
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Result: SummaryCommandResult{Summary: "短い要約"},
	})
	if next.SummaryInFlight {
		t.Error("SummaryInFlight should be false")
	}
	if next.Summary != "短い要約" {
		t.Errorf("Summary = %q", next.Summary)
	}
}

func TestClaudeHaikuEmptyResultKeepsPrev(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.Summary = "前の要約"
	cs.SummaryInFlight = true
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Result: SummaryCommandResult{Summary: ""},
	})
	if next.Summary != "前の要約" {
		t.Errorf("empty haiku erased prev: %q", next.Summary)
	}
}

func TestClaudeBranchResultMerges(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.BranchInFlight = true
	now := time.Now()
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: BranchDetectResult{Branch: "main", Background: "#F05032", Foreground: "#FFFFFF"},
	})
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false")
	}
	if next.BranchTag != "main" {
		t.Errorf("BranchTag = %q", next.BranchTag)
	}
	if next.BranchBG != "#F05032" {
		t.Errorf("BranchBG = %q", next.BranchBG)
	}
	if next.BranchFG != "#FFFFFF" {
		t.Errorf("BranchFG = %q", next.BranchFG)
	}
	if !next.BranchAt.Equal(now) {
		t.Error("BranchAt not stamped")
	}
}

func TestClaudeBranchEmptyResultPreservesExisting(t *testing.T) {
	d, cs, _ := newClaude(t)
	past := time.Now().Add(-time.Minute)
	cs.BranchTag = "feature-x"
	cs.BranchBG = "#F05032"
	cs.BranchFG = "#FFFFFF"
	cs.BranchAt = past
	cs.BranchInFlight = true

	now := time.Now()
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: BranchDetectResult{Branch: "", Background: "", Foreground: ""},
	})
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false")
	}
	if next.BranchTag != "feature-x" {
		t.Errorf("BranchTag cleared: %q", next.BranchTag)
	}
	if next.BranchBG != "#F05032" {
		t.Errorf("BranchBG cleared: %q", next.BranchBG)
	}
	if next.BranchFG != "#FFFFFF" {
		t.Errorf("BranchFG cleared: %q", next.BranchFG)
	}
	if !next.BranchAt.Equal(past) {
		t.Error("BranchAt should not be updated on empty result")
	}
}

// === Persistence ===

func TestClaudePersistRoundTrip(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs := ClaudeState{
		CommonState: CommonState{
			RoostSessionID:     "roost-1",
			WorkingDir:         "/work",
			TranscriptPath:     "/tmp/x.jsonl",
			Status:             state.StatusRunning,
			StatusChangedAt:    now,
			BranchTag:          "main",
			BranchBG:           "#F05032",
			BranchFG:           "#FFFFFF",
			BranchTarget:       "/work",
			BranchAt:           now,
			BranchIsWorktree:   true,
			BranchParentBranch: "develop",
			Summary:            "summary",
			Title:              "Refactor X",
			LastPrompt:         "do the thing",
		},
		ClaudeSessionID: "uuid-1",
	}
	bag := d.Persist(cs)
	if bag[keyRoostSessionID] != "roost-1" {
		t.Errorf("persist roost_session_id = %q", bag[keyRoostSessionID])
	}
	if bag[claudeKeyClaudeSessionID] != "uuid-1" {
		t.Errorf("persist session_id = %q", bag[claudeKeyClaudeSessionID])
	}
	if bag[keyStatus] != "running" {
		t.Errorf("persist status = %q", bag[keyStatus])
	}
	restored := d.Restore(bag, time.Now()).(ClaudeState)
	if restored.RoostSessionID != "roost-1" {
		t.Errorf("restored RoostSessionID = %q", restored.RoostSessionID)
	}
	if restored.ClaudeSessionID != cs.ClaudeSessionID {
		t.Errorf("restored ClaudeSessionID = %q", restored.ClaudeSessionID)
	}
	if restored.Status != state.StatusRunning {
		t.Errorf("restored Status = %v", restored.Status)
	}
	if !restored.StatusChangedAt.Equal(cs.StatusChangedAt) {
		t.Errorf("restored StatusChangedAt = %v", restored.StatusChangedAt)
	}
	if restored.BranchTag != "main" {
		t.Errorf("restored BranchTag = %q", restored.BranchTag)
	}
	if restored.BranchBG != "#F05032" {
		t.Errorf("restored BranchBG = %q", restored.BranchBG)
	}
	if restored.BranchFG != "#FFFFFF" {
		t.Errorf("restored BranchFG = %q", restored.BranchFG)
	}
	if !restored.BranchIsWorktree {
		t.Error("restored BranchIsWorktree = false, want true")
	}
	if restored.BranchParentBranch != "develop" {
		t.Errorf("restored BranchParentBranch = %q", restored.BranchParentBranch)
	}
	if restored.Summary != "summary" {
		t.Errorf("restored Summary = %q", restored.Summary)
	}
	if restored.Title != "Refactor X" {
		t.Errorf("restored Title = %q", restored.Title)
	}
	if restored.LastPrompt != "do the thing" {
		t.Errorf("restored LastPrompt = %q", restored.LastPrompt)
	}
}

func TestClaudeRestoreEmpty(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs := d.Restore(nil, now).(ClaudeState)
	if cs.Status != state.StatusIdle {
		t.Errorf("empty restore status = %v, want Idle", cs.Status)
	}
	if !cs.StatusChangedAt.Equal(now) {
		t.Errorf("empty restore changed_at = %v", cs.StatusChangedAt)
	}
}

// === PrepareLaunch ===

func TestClaudePrepareLaunchResume(t *testing.T) {
	home := t.TempDir()
	d := NewClaudeDriver(home, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{
		CommonState:     CommonState{WorkingDir: "/repo"},
		ClaudeSessionID: "uuid-X",
	}
	path := filepath.Join(home, ".claude", "projects", projectDir("/repo"), "uuid-X.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll error: %v", err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "claude", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "claude --resume uuid-X"
	if got != want {
		t.Errorf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestClaudePrepareLaunchNoSession(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "claude --foo", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	if got != "claude --foo" {
		t.Errorf("PrepareLaunch.Command = %q, want passthrough", got)
	}
}

func TestClaudePrepareLaunchStripsWorktree(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeCreate, "/repo", "claude --worktree", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "claude --worktree"
	if got != want {
		t.Errorf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestClaudePrepareLaunchStripsWorktreeWithName(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeCreate, "/repo", "claude --worktree my-branch", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "claude --worktree"
	if got != want {
		t.Errorf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestClaudePrepareLaunchStripsWorktreeEquals(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeCreate, "/repo", "claude --worktree=my-branch", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "claude --worktree"
	if got != want {
		t.Errorf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestClaudePrepareLaunchAddsWorktreeFlagFromOptions(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeCreate, "/repo", "claude", state.LaunchOptions{
		Worktree: state.WorktreeOption{Enabled: true},
	})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	if got := plan.Command; got != "claude --worktree" {
		t.Fatalf("PrepareLaunch.Command = %q, want %q", got, "claude --worktree")
	}
	if plan.Options.Worktree.Enabled {
		t.Fatal("PrepareLaunch.Options.Worktree.Enabled should be false")
	}
}

func TestClaudePrepareLaunchMissingTranscriptSkipsResume(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{
		CommonState:     CommonState{WorkingDir: "/repo"},
		ClaudeSessionID: "uuid-Y",
	}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "claude", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	if got != "claude" {
		t.Errorf("PrepareLaunch.Command = %q, want plain command", got)
	}
}

func TestClaudePrepareLaunchAlreadyHasResume(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-Y"}
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "claude --resume preset", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	if got != "claude --resume preset" {
		t.Errorf("PrepareLaunch.Command = %q, should not double --resume", got)
	}
}

func TestClaudePrepareCreateWithWorktree(t *testing.T) {
	d, cs, _ := newClaude(t)
	next, plan, err := d.PrepareCreate(cs, "sess-1", "/repo", "claude --worktree", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	_ = next.(ClaudeState)
	in, ok := plan.SetupJob.(WorktreeSetupInput)
	if !ok {
		t.Fatalf("SetupJob = %T", plan.SetupJob)
	}
	if len(in.CandidateNames) != worktreeNameAttempts {
		t.Fatalf("candidate names = %d, want %d", len(in.CandidateNames), worktreeNameAttempts)
	}
}

// === View ===

func TestClaudeViewNoCommandTag(t *testing.T) {
	d, cs, _ := newClaude(t)
	v := d.view(cs)
	for _, tag := range v.Card.Tags {
		if tag.Text == "claude" {
			t.Error("Tags should not contain command tag")
		}
	}
}

func TestClaudeViewDisplayName(t *testing.T) {
	d, cs, _ := newClaude(t)
	v := d.view(cs)
	if v.DisplayName != "claude" {
		t.Errorf("DisplayName = %q, want claude", v.DisplayName)
	}
}

func TestClaudeViewBorderTitle(t *testing.T) {
	d, cs, _ := newClaude(t)
	v := d.view(cs)
	if v.Card.BorderTitle.Text != "claude" {
		t.Errorf("BorderTitle.Text = %q, want claude", v.Card.BorderTitle.Text)
	}
}

func TestClaudeViewBorderBadge(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.WorkingDir = "/home/test/project"
	v := d.view(cs)
	if v.Card.BorderBadge != "~/project" {
		t.Errorf("BorderBadge = %q, want ~/project", v.Card.BorderBadge)
	}
}

func TestClaudeViewBorderBadgeDeepPath(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.WorkingDir = "/home/test/code/go/agent-roost"
	v := d.view(cs)
	if v.Card.BorderBadge != "~/c/g/agent-roost" {
		t.Errorf("BorderBadge = %q, want ~/c/g/agent-roost", v.Card.BorderBadge)
	}
}

func TestClaudeViewBranchTagWhenSet(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.BranchTag = "feat-x"
	v := d.view(cs)
	if len(v.Card.Tags) < 1 || v.Card.Tags[0].Text != "feat-x" {
		t.Errorf("branch tag missing: %+v", v.Card.Tags)
	}
}

func TestClaudeViewBranchTagWorktree(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.BranchTag = "feature"
	cs.BranchParentBranch = "main"
	v := d.view(cs)
	if len(v.Card.Tags) < 1 || v.Card.Tags[0].Text != "feature \u2192 main" {
		t.Errorf("worktree branch tag: %+v", v.Card.Tags)
	}
}

func TestClaudeViewSubtitleFallsBackToLastPrompt(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.LastPrompt = "the prompt"
	v := d.view(cs)
	if v.Card.Subtitle != "the prompt" {
		t.Errorf("Subtitle = %q, want the prompt", v.Card.Subtitle)
	}
	cs.Summary = "the summary"
	v = d.view(cs)
	if v.Card.Subtitle != "the summary" {
		t.Errorf("Subtitle = %q, want the summary (summary should win)", v.Card.Subtitle)
	}
}

func TestClaudeViewIndicatorsCurrentTool(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.CurrentTool = "Edit"
	v := d.view(cs)
	if len(v.Card.Indicators) != 1 || v.Card.Indicators[0] != "▸ Edit" {
		t.Errorf("Indicators = %v", v.Card.Indicators)
	}
}

func TestClaudeViewIndicatorsSubagents(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.SubagentCounts = map[string]int{"a": 2, "b": 1}
	v := d.view(cs)
	if len(v.Card.Indicators) == 0 {
		t.Fatal("expected subagent indicator")
	}
	if v.Card.Indicators[0] != "3 subs" {
		t.Errorf("indicator = %q, want %q", v.Card.Indicators[0], "3 subs")
	}
}

func TestClaudeViewLogTabsTranscript(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptPath = "/tmp/x.jsonl"
	v := d.view(cs)
	if len(v.LogTabs) == 0 {
		t.Fatal("expected TRANSCRIPT tab")
	}
	if v.LogTabs[0].Label != "TRANSCRIPT" || v.LogTabs[0].Kind != transcript.KindTranscript {
		t.Errorf("LogTab[0] = %+v", v.LogTabs[0])
	}
}

func TestClaudeViewEventsTab(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptPath = "/tmp/x.jsonl"
	cs.RoostSessionID = "sess-1"
	v := d.view(cs)
	if len(v.LogTabs) < 2 {
		t.Fatalf("expected TRANSCRIPT + EVENTS tabs, got %d", len(v.LogTabs))
	}
	ev := v.LogTabs[1]
	if ev.Label != "EVENTS" {
		t.Errorf("LogTab[1].Label = %q, want EVENTS", ev.Label)
	}
	if ev.Path != "/data/events/sess-1.log" {
		t.Errorf("LogTab[1].Path = %q, want /data/events/sess-1.log", ev.Path)
	}
	if ev.Kind != state.TabKindText {
		t.Errorf("LogTab[1].Kind = %q, want text", ev.Kind)
	}
}

func TestClaudeViewEventsTabOmittedWithoutRoostSessionID(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptPath = "/tmp/x.jsonl"
	v := d.view(cs)
	for _, tab := range v.LogTabs {
		if tab.Label == "EVENTS" {
			t.Error("EVENTS tab should not appear without RoostSessionID")
		}
	}
}

func TestClaudeViewInfoExtras(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.Title = "T"
	cs.WorkingDir = "/w"
	v := d.view(cs)
	wantLabels := map[string]string{"Title": "T", "Working Dir": "/w"}
	got := map[string]string{}
	for _, l := range v.InfoExtras {
		got[l.Label] = l.Value
	}
	for k, v := range wantLabels {
		if got[k] != v {
			t.Errorf("InfoExtras[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// === Step end-to-end ===

func TestClaudeStepRoundTripSessionStartThenView(t *testing.T) {
	d, cs, now := newClaude(t)
	next, effs, view := d.Step(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/work",
		"transcript_path": "/tmp/x.jsonl",
		"hook_event_name": "SessionStart",
	}, now))
	cs2 := next.(ClaudeState)
	if cs2.ClaudeSessionID != "uuid" {
		t.Error("Step did not propagate session id")
	}
	if len(effs) == 0 {
		t.Error("Step returned no effects")
	}
	if view.DisplayName != "claude" {
		t.Errorf("Step returned DisplayName = %q, want claude", view.DisplayName)
	}
}

func TestResolveTranscriptPathFallback(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{
		CommonState: CommonState{
			WorkingDir: "/some/work",
		},
		ClaudeSessionID: "uuid-Z",
	}
	got := d.resolveTranscriptPath(cs)
	want := "/home/test/.claude/projects/-some-work/uuid-Z.jsonl"
	if got != want {
		t.Errorf("resolveTranscriptPath = %q, want %q", got, want)
	}
}

func TestClaudeNoStateChangeEventsStillLog(t *testing.T) {
	events := []string{"SubagentStop", "PostToolUseFailure", "PreCompact", "PostCompact", "TaskCreated", "TaskCompleted"}
	for _, name := range events {
		t.Run(name, func(t *testing.T) {
			d, cs, now := newClaude(t)
			cs.ClaudeSessionID = "uuid"
			prevStatus := cs.Status
			next, effs := d.handleHook(cs, hookEvent(name, map[string]string{
				"session_id":      "uuid",
				"hook_event_name": name,
			}, now))
			if next.Status != prevStatus {
				t.Errorf("Status changed to %v, want %v (unchanged)", next.Status, prevStatus)
			}
			logEff, ok := findEffect[state.EffEventLogAppend](effs)
			if !ok {
				t.Fatalf("expected EffEventLogAppend for %s", name)
			}
			if logEff.Line != name {
				t.Errorf("log line = %q, want %q", logEff.Line, name)
			}
		})
	}
}

func TestResolveTranscriptPathPrefersExplicit(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{
		CommonState: CommonState{
			TranscriptPath: "/explicit/path.jsonl",
			WorkingDir:     "/w",
		},
		ClaudeSessionID: "u",
	}
	got := d.resolveTranscriptPath(cs)
	if got != "/explicit/path.jsonl" {
		t.Errorf("resolveTranscriptPath = %q, want explicit", got)
	}
}

// === Hang detection (pane capture) ===

func TestClaudeTickEmitsCapturePaneWhenBackgroundRunning(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	next, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: false, PaneTarget: "42",
	})
	if !next.CaptureInFlight {
		t.Error("CaptureInFlight should be true")
	}
	job, ok := findEffect[state.EffStartJob](effs)
	if !ok {
		t.Fatal("expected EffStartJob for CapturePaneInput")
	}
	cp, ok := job.Input.(CapturePaneInput)
	if !ok {
		t.Fatalf("job input type = %T, want CapturePaneInput", job.Input)
	}
	if cp.PaneTarget != "42" {
		t.Errorf("PaneTarget = %q, want 42", cp.PaneTarget)
	}
}

func TestClaudeTickSkipsCaptureWhenActive(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	_, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: true, PaneTarget: "42",
	})
	for _, e := range effs {
		if j, ok := e.(state.EffStartJob); ok {
			if _, ok := j.Input.(CapturePaneInput); ok {
				t.Error("should not capture pane for active session")
			}
		}
	}
}

func TestClaudeTickSkipsCaptureWhenNotRunning(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusWaiting
	_, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: false, PaneTarget: "42",
	})
	for _, e := range effs {
		if j, ok := e.(state.EffStartJob); ok {
			if _, ok := j.Input.(CapturePaneInput); ok {
				t.Error("should not capture pane when not Running")
			}
		}
	}
}

func TestClaudeTickSkipsCaptureWhenInFlight(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	cs.CaptureInFlight = true
	_, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: false, PaneTarget: "42",
	})
	for _, e := range effs {
		if j, ok := e.(state.EffStartJob); ok {
			if _, ok := j.Input.(CapturePaneInput); ok {
				t.Error("should not schedule duplicate capture")
			}
		}
	}
}

func TestClaudeCapturePrimesBaseline(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.CaptureInFlight = true
	now := time.Date(2026, 4, 10, 12, 1, 0, 0, time.UTC)
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: CapturePaneResult{Content: "hello", Hash: "abc123"},
	})
	if next.CaptureInFlight {
		t.Error("CaptureInFlight should be cleared")
	}
	if next.PaneHash != "abc123" {
		t.Errorf("PaneHash = %q, want abc123", next.PaneHash)
	}
	if !next.PaneHashAt.Equal(now) {
		t.Error("PaneHashAt should be set to now")
	}
	if next.Status != state.StatusIdle { // default from newClaude
		t.Error("status should not change on priming")
	}
}

func TestClaudeCaptureResetsTimer(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.CaptureInFlight = true
	cs.PaneHash = "old-hash"
	cs.PaneHashAt = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 10, 12, 1, 0, 0, time.UTC)
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: CapturePaneResult{Content: "changed", Hash: "new-hash"},
	})
	if next.PaneHash != "new-hash" {
		t.Errorf("PaneHash = %q, want new-hash", next.PaneHash)
	}
	if !next.PaneHashAt.Equal(now) {
		t.Error("PaneHashAt should be updated on hash change")
	}
}

func TestClaudeCaptureUnchangedKeepsTimer(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.CaptureInFlight = true
	orig := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs.PaneHash = "same-hash"
	cs.PaneHashAt = orig
	now := time.Date(2026, 4, 10, 12, 1, 0, 0, time.UTC)
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: CapturePaneResult{Content: "same", Hash: "same-hash"},
	})
	if !next.PaneHashAt.Equal(orig) {
		t.Error("PaneHashAt should not change when hash is same")
	}
}

func TestClaudeHangDetectionTriggersIdle(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.Status = state.StatusRunning
	cs.StatusChangedAt = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs.PaneHash = "stale"
	cs.PaneHashAt = cs.StatusChangedAt

	// Tick at threshold+1s
	now := cs.StatusChangedAt.Add(claudeHangThreshold + time.Second)
	next, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: false, PaneTarget: "42",
	})
	if next.Status != state.StatusIdle {
		t.Errorf("Status = %v, want Idle", next.Status)
	}
	if !next.HangDetected {
		t.Error("HangDetected should be true")
	}
	if !next.StatusChangedAt.Equal(now) {
		t.Error("StatusChangedAt should be updated")
	}
	if _, ok := findEffect[state.EffEventLogAppend](effs); !ok {
		t.Error("expected EffEventLogAppend for hang detection")
	}
}

func TestClaudeHangDetectionSuppressedBySubagents(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.Status = state.StatusRunning
	cs.StatusChangedAt = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs.PaneHash = "stale"
	cs.PaneHashAt = cs.StatusChangedAt
	cs.SubagentCounts = map[string]int{"Task": 2}

	now := cs.StatusChangedAt.Add(claudeHangThreshold + time.Minute)
	next, _ := d.handleTick(cs, state.DEvTick{
		Now: now, Active: false, PaneTarget: "42",
	})
	if next.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running (subagents active)", next.Status)
	}
	if next.HangDetected {
		t.Error("HangDetected should be false when subagents active")
	}
}

func TestClaudeHookResetsHangDetected(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.HangDetected = true
	cs.PaneHash = "stale"
	next, _ := d.handleHook(cs, hookEvent("PreToolUse", map[string]string{
		"session_id":      "uuid",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
	}, now))
	if next.HangDetected {
		t.Error("HangDetected should be cleared on hook")
	}
	if next.PaneHash != "" {
		t.Errorf("PaneHash should be reset, got %q", next.PaneHash)
	}
}

func TestClaudeStaleHookDoesNotResetHangDetected(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.LastBridgeTS = now.Add(200 * time.Millisecond)
	cs.HangDetected = true
	cs.PaneHash = "stale-hash"

	ev := hookEvent("Stop", map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	ev.Timestamp = now.Add(100 * time.Millisecond) // stale
	next, _ := d.handleHook(cs, ev)
	if !next.HangDetected {
		t.Error("stale hook should not clear HangDetected")
	}
	if next.PaneHash != "stale-hash" {
		t.Error("stale hook should not clear PaneHash")
	}
}

func TestClaudeViewIndicatorsStale(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.HangDetected = true
	v := d.view(cs)
	if len(v.Card.Indicators) == 0 || v.Card.Indicators[0] != "stale?" {
		t.Errorf("Indicators = %v, want [stale? ...]", v.Card.Indicators)
	}
}

func TestClaudeStateChangeDoesNotUpdateWorkingDir(t *testing.T) {
	d, cs, now := newClaude(t)
	cs, _ = d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/original",
		"hook_event_name": "SessionStart",
	}, now))
	if cs.WorkingDir != "/original" {
		t.Fatalf("WorkingDir after SessionStart = %q, want /original", cs.WorkingDir)
	}
	cs, _ = d.handleHook(cs, hookEvent("Stop", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/changed",
		"hook_event_name": "Stop",
	}, now.Add(time.Second)))
	if cs.WorkingDir != "/original" {
		t.Errorf("WorkingDir after Stop = %q, want /original (should not change)", cs.WorkingDir)
	}
}

func TestClaudeUserPromptSubmitDoesNotUpdateWorkingDir(t *testing.T) {
	d, cs, now := newClaude(t)
	cs, _ = d.handleHook(cs, hookEvent("SessionStart", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/original",
		"hook_event_name": "SessionStart",
	}, now))
	cs, _ = d.handleHook(cs, hookEvent("UserPromptSubmit", map[string]string{
		"session_id":      "uuid",
		"cwd":             "/worktree/path",
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "hello",
	}, now.Add(time.Second)))
	if cs.WorkingDir != "/original" {
		t.Errorf("WorkingDir after UserPromptSubmit = %q, want /original (should not change)", cs.WorkingDir)
	}
}
