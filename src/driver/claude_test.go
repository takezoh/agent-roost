package driver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/lib/claude/transcript"
	"github.com/takezoh/agent-roost/state"
)

const testHome = "/home/test"
const testEventLogDir = "/data/events"

// hookPayloadRaw builds a DEvHook payload with a "raw" JSON key from
// the given fields. Extra kv pairs can be added via the extras map.
// The "now" key is passed outside "raw" since it's a time.Time that
// the reducer injects, not part of the Claude hook JSON.
func hookPayloadRaw(fields map[string]string, now time.Time) map[string]any {
	raw, _ := json.Marshal(fields)
	m := map[string]any{"raw": string(raw)}
	if !now.IsZero() {
		m["now"] = now
	}
	return m
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
	next, effs := d.handleHook(cs, state.DEvHook{
		Event: "SessionStart",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "claude-uuid",
			"cwd":             "/work",
			"transcript_path": "/tmp/x.jsonl",
			"hook_event_name": "SessionStart",
		}, now),
	})
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
	next, effs := d.handleHook(cs, state.DEvHook{
		Event: "SessionStart",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"cwd":             "/work",
			"hook_event_name": "SessionStart",
		}, now),
	})
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
	next, _ := d.handleHook(cs, state.DEvHook{
		Event: "SessionStart",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"hook_event_name": "SessionStart",
		}, now),
	})
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false when cwd is empty")
	}
}

func TestClaudeSessionStartAbsorbsRoostSessionID(t *testing.T) {
	d, cs, now := newClaude(t)
	payload := hookPayloadRaw(map[string]string{
		"session_id":      "claude-uuid",
		"hook_event_name": "SessionStart",
	}, now)
	payload["roost_session_id"] = "roost-abc"
	next, _ := d.handleHook(cs, state.DEvHook{
		Event:   "SessionStart",
		Payload: payload,
	})
	if next.RoostSessionID != "roost-abc" {
		t.Errorf("RoostSessionID = %q, want roost-abc", next.RoostSessionID)
	}
}

func TestClaudeStateChangeStopSetsWaiting(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"
	next, effs := d.handleHook(cs, state.DEvHook{
		Event: "Stop",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"hook_event_name": "Stop",
		}, now.Add(time.Second)),
	})
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
	next, _ := d.handleHook(cs, state.DEvHook{
		Event:   "Notification",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":        "uuid",
			"hook_event_name":   "Notification",
			"notification_type": "something_else",
		}, time.Time{}),
	})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (unchanged)", next.Status)
	}
}

func TestClaudeUserPromptSubmitTriggersHaiku(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleHook(cs, state.DEvHook{
		Event: "UserPromptSubmit",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"hook_event_name": "UserPromptSubmit",
			"prompt":          "do the thing",
		}, now),
	})
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
	in, ok := job.Input.(HaikuSummaryInput)
	if !ok {
		t.Fatalf("job input type = %T, want HaikuSummaryInput", job.Input)
	}
	if in.CurrentPrompt != "do the thing" {
		t.Errorf("haiku CurrentPrompt = %q, want %q", in.CurrentPrompt, "do the thing")
	}
	if in.ClaudeUUID != "uuid" {
		t.Errorf("haiku ClaudeUUID = %q, want uuid", in.ClaudeUUID)
	}
}

func TestClaudeUserPromptSubmitDedupesWhileInFlight(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.SummaryInFlight = true
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleHook(cs, state.DEvHook{
		Event: "UserPromptSubmit",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"hook_event_name": "UserPromptSubmit",
			"prompt":          "another",
		}, now),
	})
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

	// First event at bridge_ts=200
	p1 := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	p1["bridge_ts"] = int64(200)
	next, _ := d.handleHook(cs, state.DEvHook{Event: "Stop", Payload: p1})
	if next.LastBridgeTS != 200 {
		t.Fatalf("LastBridgeTS = %d, want 200", next.LastBridgeTS)
	}
	if next.Status != state.StatusWaiting {
		t.Fatalf("Status = %v, want Waiting", next.Status)
	}

	// Second event at bridge_ts=100 (stale) — must be dropped
	p2 := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "UserPromptSubmit",
		"prompt": "stale",
	}, now)
	p2["bridge_ts"] = int64(100)
	next2, effs := d.handleHook(next, state.DEvHook{Event: "UserPromptSubmit", Payload: p2})
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
	cs.LastBridgeTS = 500

	p := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	// No bridge_ts key — should be accepted for backward compat
	next, _ := d.handleHook(cs, state.DEvHook{Event: "Stop", Payload: p})
	if next.Status != state.StatusWaiting {
		t.Errorf("missing bridge_ts should be accepted, got status %v", next.Status)
	}
	if next.LastBridgeTS != 500 {
		t.Errorf("LastBridgeTS changed to %d, should stay 500", next.LastBridgeTS)
	}
}

func TestClaudeSessionStartBypassesOrdering(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.LastBridgeTS = 9000 // high watermark from previous session

	// SessionStart with a lower bridge_ts (e.g. clock skew after NTP)
	p := hookPayloadRaw(map[string]string{
		"session_id":      "uuid",
		"cwd":             "/work",
		"transcript_path": "/tmp/x.jsonl",
		"hook_event_name": "SessionStart",
	}, now)
	p["bridge_ts"] = int64(100)
	next, effs := d.handleHook(cs, state.DEvHook{Event: "SessionStart", Payload: p})
	if next.Status != state.StatusIdle {
		t.Errorf("SessionStart should always be accepted, got status %v", next.Status)
	}
	if next.LastBridgeTS != 100 {
		t.Errorf("LastBridgeTS = %d, want 100 (reset by SessionStart)", next.LastBridgeTS)
	}
	if len(effs) == 0 {
		t.Error("SessionStart should produce effects")
	}
}

func TestClaudeHookAcceptsFloat64BridgeTS(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"

	p := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	p["bridge_ts"] = float64(300)
	next, _ := d.handleHook(cs, state.DEvHook{Event: "Stop", Payload: p})
	if next.LastBridgeTS != 300 {
		t.Errorf("LastBridgeTS = %d, want 300 (from float64)", next.LastBridgeTS)
	}
}

func TestClaudeHookPreToolUseThenNotification(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"

	// PreToolUse arrives first — status becomes Running.
	p1 := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "PreToolUse",
		"tool_name": "Bash",
	}, now)
	p1["bridge_ts"] = int64(100)
	next, _ := d.handleHook(cs, state.DEvHook{Event: "PreToolUse", Payload: p1})
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want Running", next.Status)
	}

	// Notification (permission_prompt) arrives with a higher bridge_ts
	// — must NOT be dropped. Before the bridge_ts-before-stdin fix,
	// the Notification process could get a lower timestamp than the
	// PreToolUse process due to stdin read latency, causing it to be
	// dropped as stale and leaving the session stuck at Running.
	p2 := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Notification",
		"notification_type": "permission_prompt",
	}, now)
	p2["bridge_ts"] = int64(150)
	next2, _ := d.handleHook(next, state.DEvHook{Event: "Notification", Payload: p2})
	if next2.Status != state.StatusPending {
		t.Errorf("Notification should advance to Pending, got %v", next2.Status)
	}
}

func TestClaudeHookSubagentEventsDoNotChangeStatus(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.ClaudeSessionID = "uuid"
	cs.TranscriptPath = "/tmp/t.jsonl"

	// Set pending via permission_prompt.
	p0 := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Notification",
		"notification_type": "permission_prompt",
	}, now)
	p0["bridge_ts"] = int64(100)
	cs, _ = d.handleHook(cs, state.DEvHook{Event: "Notification", Payload: p0})
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
			p := hookPayloadRaw(fields, now)
			p["bridge_ts"] = int64(200)
			next, effs := d.handleHook(cs, state.DEvHook{Event: tt.event, Payload: p})
			if next.Status != state.StatusPending {
				t.Errorf("%s %s: Status = %v, want Pending", tt.event, tt.toolName, next.Status)
			}
			if _, ok := findEffect[state.EffEventLogAppend](effs); !ok {
				t.Errorf("%s %s: expected EffEventLogAppend", tt.event, tt.toolName)
			}
		})
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

// === Transcript change handling ===

func TestClaudeTranscriptChangedSchedulesParse(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.TranscriptPath = "/tmp/x.jsonl"
	cs.ClaudeSessionID = "uuid"
	next, effs := d.handleTranscriptChanged(cs, state.DEvFileChanged{Path: "/tmp/x.jsonl"})
	if !next.TranscriptInFlight {
		t.Error("TranscriptInFlight should be true")
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
	_, effs := d.handleTranscriptChanged(cs, state.DEvFileChanged{Path: "/tmp/x.jsonl"})
	if len(effs) != 0 {
		t.Errorf("dedup effs = %d, want 0", len(effs))
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
		Result: HaikuSummaryResult{Summary: "短い要約"},
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
		Result: HaikuSummaryResult{Summary: ""},
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
		RoostSessionID:  "roost-1",
		ClaudeSessionID: "uuid-1",
		WorkingDir:      "/work",
		TranscriptPath:  "/tmp/x.jsonl",
		Status:          state.StatusRunning,
		StatusChangedAt: now,
		BranchTag:       "main",
		BranchBG:        "#F05032",
		BranchFG:        "#FFFFFF",
		BranchTarget:    "/work",
		BranchAt:        now,
		Summary:         "summary",
		Title:           "Refactor X",
		LastPrompt:      "do the thing",
	}
	bag := d.Persist(cs)
	if bag[claudeKeyRoostSessionID] != "roost-1" {
		t.Errorf("persist roost_session_id = %q", bag[claudeKeyRoostSessionID])
	}
	if bag[claudeKeyClaudeSessionID] != "uuid-1" {
		t.Errorf("persist session_id = %q", bag[claudeKeyClaudeSessionID])
	}
	if bag[claudeKeyStatus] != "running" {
		t.Errorf("persist status = %q", bag[claudeKeyStatus])
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

// === SpawnCommand ===

func TestClaudeSpawnCommandResume(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-X"}
	got := d.SpawnCommand(cs, "claude")
	want := "claude --resume uuid-X"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandNoSession(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{}
	got := d.SpawnCommand(cs, "claude --foo")
	if got != "claude --foo" {
		t.Errorf("SpawnCommand = %q, want passthrough", got)
	}
}

func TestClaudeSpawnCommandStripsWorktree(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandStripsWorktreeWithName(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree my-branch")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandStripsWorktreeEquals(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree=my-branch")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandAlreadyHasResume(t *testing.T) {
	d := NewClaudeDriver(testHome, testEventLogDir, ClaudeOptions{})
	cs := ClaudeState{ClaudeSessionID: "uuid-Y"}
	got := d.SpawnCommand(cs, "claude --resume preset")
	if got != "claude --resume preset" {
		t.Errorf("SpawnCommand = %q, should not double --resume", got)
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
	next, effs, view := d.Step(cs, state.DEvHook{
		Event: "SessionStart",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"cwd":             "/work",
			"transcript_path": "/tmp/x.jsonl",
			"hook_event_name": "SessionStart",
		}, now),
	})
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
		ClaudeSessionID: "uuid-Z",
		WorkingDir:      "/some/work",
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
			next, effs := d.handleHook(cs, state.DEvHook{
				Event: name,
				Payload: hookPayloadRaw(map[string]string{
					"session_id":      "uuid",
					"hook_event_name": name,
				}, now),
			})
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
		TranscriptPath:  "/explicit/path.jsonl",
		ClaudeSessionID: "u",
		WorkingDir:      "/w",
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
		Now: now, Active: false, WindowID: "@42",
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
	if cp.WindowID != "@42" {
		t.Errorf("WindowID = %q, want @42", cp.WindowID)
	}
}

func TestClaudeTickSkipsCaptureWhenActive(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	_, effs := d.handleTick(cs, state.DEvTick{
		Now: now, Active: true, WindowID: "@42",
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
		Now: now, Active: false, WindowID: "@42",
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
		Now: now, Active: false, WindowID: "@42",
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
		Now: now, Active: false, WindowID: "@42",
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
		Now: now, Active: false, WindowID: "@42",
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
	next, _ := d.handleHook(cs, state.DEvHook{
		Event: "PreToolUse",
		Payload: hookPayloadRaw(map[string]string{
			"session_id":      "uuid",
			"hook_event_name": "PreToolUse",
			"tool_name":       "Bash",
		}, now),
	})
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
	cs.LastBridgeTS = 200
	cs.HangDetected = true
	cs.PaneHash = "stale-hash"

	p := hookPayloadRaw(map[string]string{
		"session_id": "uuid", "hook_event_name": "Stop",
	}, now)
	p["bridge_ts"] = int64(100) // stale
	next, _ := d.handleHook(cs, state.DEvHook{Event: "Stop", Payload: p})
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
