package driver

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

const testHome = "/home/test"

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
	d := NewClaudeDriver(testHome)
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

// === Tick handling (branch detection) ===

func TestClaudeTickInactiveDoesNothing(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.WorkingDir = "/work"
	_, effs := d.handleTick(cs, state.DEvTick{Now: now, Active: false})
	if len(effs) != 0 {
		t.Errorf("inactive tick effs = %d, want 0", len(effs))
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
	if _, ok := job.Input.(GitBranchInput); !ok {
		t.Errorf("job input type = %T, want GitBranchInput", job.Input)
	}
	in, ok := job.Input.(GitBranchInput)
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
		Result: GitBranchResult{Branch: "main"},
	})
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false")
	}
	if next.BranchTag != "main" {
		t.Errorf("BranchTag = %q", next.BranchTag)
	}
	if !next.BranchAt.Equal(now) {
		t.Error("BranchAt not stamped")
	}
}

// === Persistence ===

func TestClaudePersistRoundTrip(t *testing.T) {
	d := NewClaudeDriver(testHome)
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cs := ClaudeState{
		ClaudeSessionID: "uuid-1",
		WorkingDir:      "/work",
		TranscriptPath:  "/tmp/x.jsonl",
		Status:          state.StatusRunning,
		StatusChangedAt: now,
		BranchTag:       "main",
		BranchTarget:    "/work",
		BranchAt:        now,
		Summary:         "summary",
		Title:           "Refactor X",
		LastPrompt:      "do the thing",
	}
	bag := d.Persist(cs)
	if bag[claudeKeyClaudeSessionID] != "uuid-1" {
		t.Errorf("persist session_id = %q", bag[claudeKeyClaudeSessionID])
	}
	if bag[claudeKeyStatus] != "running" {
		t.Errorf("persist status = %q", bag[claudeKeyStatus])
	}
	restored := d.Restore(bag, time.Now()).(ClaudeState)
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
	d := NewClaudeDriver(testHome)
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
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{ClaudeSessionID: "uuid-X"}
	got := d.SpawnCommand(cs, "claude")
	want := "claude --resume uuid-X"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandNoSession(t *testing.T) {
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{}
	got := d.SpawnCommand(cs, "claude --foo")
	if got != "claude --foo" {
		t.Errorf("SpawnCommand = %q, want passthrough", got)
	}
}

func TestClaudeSpawnCommandStripsWorktree(t *testing.T) {
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandStripsWorktreeWithName(t *testing.T) {
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree my-branch")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandStripsWorktreeEquals(t *testing.T) {
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{ClaudeSessionID: "uuid-W"}
	got := d.SpawnCommand(cs, "claude --worktree=my-branch")
	want := "claude --resume uuid-W"
	if got != want {
		t.Errorf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestClaudeSpawnCommandAlreadyHasResume(t *testing.T) {
	d := NewClaudeDriver(testHome)
	cs := ClaudeState{ClaudeSessionID: "uuid-Y"}
	got := d.SpawnCommand(cs, "claude --resume preset")
	if got != "claude --resume preset" {
		t.Errorf("SpawnCommand = %q, should not double --resume", got)
	}
}

// === View ===

func TestClaudeViewIncludesCommandTag(t *testing.T) {
	d, cs, _ := newClaude(t)
	v := d.view(cs)
	if len(v.Card.Tags) == 0 {
		t.Fatal("expected at least one tag")
	}
	if v.Card.Tags[0].Text != "claude" {
		t.Errorf("first tag = %q, want claude", v.Card.Tags[0].Text)
	}
}

func TestClaudeViewBranchTagWhenSet(t *testing.T) {
	d, cs, _ := newClaude(t)
	cs.BranchTag = "feat-x"
	v := d.view(cs)
	if len(v.Card.Tags) < 2 || v.Card.Tags[1].Text != "feat-x" {
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
	if v.LogTabs[0].Label != "TRANSCRIPT" || v.LogTabs[0].Kind != state.TabKindTranscript {
		t.Errorf("LogTab[0] = %+v", v.LogTabs[0])
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
	if len(view.Card.Tags) == 0 {
		t.Error("Step returned empty view tags")
	}
}

func TestResolveTranscriptPathFallback(t *testing.T) {
	d := NewClaudeDriver(testHome)
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

func TestResolveTranscriptPathPrefersExplicit(t *testing.T) {
	d := NewClaudeDriver(testHome)
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
