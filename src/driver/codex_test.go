package driver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	codextranscript "github.com/takezoh/agent-roost/lib/codex/transcript"
	"github.com/takezoh/agent-roost/state"
)

func codexHook(fields map[string]string, ts time.Time) state.DEvHook {
	raw, _ := json.Marshal(fields)
	return state.DEvHook{Payload: raw, Timestamp: ts}
}

func newCodex(t *testing.T) (CodexDriver, CodexState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	d := NewCodexDriver("/tmp/events")
	cs := d.NewState(now).(CodexState)
	return d, cs, now
}

func TestCodexSessionStartSetsIdle(t *testing.T) {
	d, cs, now := newCodex(t)
	ev := codexHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "SessionStart",
		"cwd":             "/repo",
		"transcript_path": "/tmp/t.jsonl",
		"source":          "resume",
	}, now)
	ev.RoostSessionID = "r1"
	next, effs := d.handleHook(cs, state.FrameContext{IsRoot: true}, ev)

	if next.Status != state.StatusIdle {
		t.Fatalf("Status = %v, want idle", next.Status)
	}
	if next.CodexSessionID != "sess-1" {
		t.Fatalf("CodexSessionID = %q", next.CodexSessionID)
	}
	if next.RoostSessionID != "r1" {
		t.Fatalf("RoostSessionID = %q", next.RoostSessionID)
	}
	if next.StartDir != "/repo" || next.TranscriptPath != "/tmp/t.jsonl" {
		t.Fatalf("working data not absorbed: %+v", next)
	}
	if len(effs) != 4 {
		t.Fatalf("effects = %d, want 4", len(effs))
	}
	foundBranch := false
	foundWatch := false
	foundTranscriptParse := false
	for _, eff := range effs {
		if watch, ok := eff.(state.EffWatchFile); ok {
			foundWatch = watch.Path == "/tmp/t.jsonl"
		}
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			foundBranch = true
		}
		if _, ok := job.Input.(CodexTranscriptParseInput); ok {
			foundTranscriptParse = true
		}
	}
	if !foundBranch {
		t.Fatal("expected BranchDetectInput job")
	}
	if !foundWatch {
		t.Fatal("expected EffWatchFile")
	}
	if !foundTranscriptParse {
		t.Fatal("expected CodexTranscriptParseInput job")
	}
}

func TestCodexSessionStartNonRootSkipsBranchDetect(t *testing.T) {
	d, cs, now := newCodex(t)
	ev := codexHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "SessionStart",
		"cwd":             "/repo",
		"transcript_path": "/tmp/t.jsonl",
	}, now)
	ev.RoostSessionID = "r1"
	next, effs := d.handleHook(cs, state.FrameContext{IsRoot: false}, ev)
	// Non-root: BranchDetect must NOT be emitted.
	if next.BranchInFlight {
		t.Error("BranchInFlight should be false for non-root frame")
	}
	for _, eff := range effs {
		if j, ok := eff.(state.EffStartJob); ok {
			if _, ok := j.Input.(BranchDetectInput); ok {
				t.Error("non-root SessionStart must not emit BranchDetectInput")
			}
		}
	}
	// Other SessionStart work (transcript watch + parse) must still run.
	foundWatch := false
	for _, eff := range effs {
		if w, ok := eff.(state.EffWatchFile); ok {
			foundWatch = w.Path == "/tmp/t.jsonl"
		}
	}
	if !foundWatch {
		t.Error("non-root SessionStart should still emit EffWatchFile for transcript")
	}
}

func TestCodexUserPromptTransitionsRunning(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.RecentTurns = []SummaryTurn{
		{Role: "user", Text: "inspect repo"},
		{Role: "assistant", Text: "checking files"},
		{Role: "user", Text: "find failing tests"},
		{Role: "assistant", Text: "found driver failures"},
	}
	next, effs := d.handleHook(cs, state.FrameContext{IsRoot: true}, codexHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "implement this",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
	if next.LastPrompt != "implement this" {
		t.Fatalf("LastPrompt = %q", next.LastPrompt)
	}
	if !next.SummaryInFlight {
		t.Fatal("SummaryInFlight should be true")
	}
	var summaryJob SummaryCommandInput
	foundSummary := false
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		in, ok := job.Input.(SummaryCommandInput)
		if ok {
			summaryJob = in
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatal("expected SummaryCommandInput job")
	}
	if strings.Contains(summaryJob.Prompt, "inspect repo") {
		t.Fatalf("prompt should keep only last 2 user turns: %q", summaryJob.Prompt)
	}
	if !strings.Contains(summaryJob.Prompt, "find failing tests") || !strings.Contains(summaryJob.Prompt, "implement this") {
		t.Fatalf("summary prompt missing recent user turns: %q", summaryJob.Prompt)
	}
}

func TestCodexStopTransitionsWaiting(t *testing.T) {
	d, cs, now := newCodex(t)
	next, _ := d.handleHook(cs, state.FrameContext{IsRoot: true}, codexHook(map[string]string{
		"session_id":             "sess-1",
		"hook_event_name":        "Stop",
		"last_assistant_message": "done",
		"stop_reason":            "finished",
	}, now))
	if next.Status != state.StatusWaiting {
		t.Fatalf("Status = %v, want waiting", next.Status)
	}
	if next.LastAssistantMessage != "done" {
		t.Fatalf("LastAssistantMessage = %q", next.LastAssistantMessage)
	}
}

func TestCodexPendingTransitionsToRunningOnPreToolUse(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.Status = state.StatusPending
	cs.StatusChangedAt = now.Add(-time.Minute)
	next, _ := d.handleHook(cs, state.FrameContext{IsRoot: true}, codexHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
}

func TestCodexDropsStaleHook(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.LastHookAt = now
	next, effs := d.handleHook(cs, state.FrameContext{IsRoot: true}, codexHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "Stop",
	}, now))
	if next.Status != cs.Status {
		t.Fatal("stale hook should not update status")
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
}

func TestCodexPrepareLaunchResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "codex --model gpt-5-codex", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "codex --model gpt-5-codex resume abc-123"
	if got != want {
		t.Fatalf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestCodexPrepareLaunchNoDoubleResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "codex resume abc", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	if got != "codex resume abc" {
		t.Fatalf("PrepareLaunch.Command = %q", got)
	}
}

func TestCodexPrepareLaunchStripsWorktreeOnResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-1234"
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "codex --worktree feature --model gpt-5-codex", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "codex --model gpt-5-codex resume abc-123"
	if got != want {
		t.Fatalf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestCodexPrepareLaunchStripsWorktreeWithoutResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-1234"
	plan, err := d.PrepareLaunch(cs, state.LaunchModeCreate, "/repo", "codex --worktree feature --model gpt-5-codex", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	want := "codex --model gpt-5-codex"
	if got != want {
		t.Fatalf("PrepareLaunch.Command = %q, want %q", got, want)
	}
}

func TestCodexPrepareLaunchSkipsNonCodexBaseCommand(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	plan, err := d.PrepareLaunch(cs, state.LaunchModeColdStart, "/repo", "env FOO=bar", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	got := plan.Command
	if got != "env FOO=bar" {
		t.Fatalf("PrepareLaunch.Command = %q", got)
	}
}

func TestCodexPersistRestoreRoundTrip(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.CommonState = CommonState{
		RoostSessionID:       "r1",
		StartDir:             "/repo",
		TranscriptPath:       "/repo/t.jsonl",
		WorktreeName:         "codex-abcd",
		Status:               state.StatusRunning,
		StatusChangedAt:      now,
		BranchTag:            "main",
		BranchBG:             "#111111",
		BranchFG:             "#ffffff",
		BranchTarget:         "/repo",
		BranchAt:             now,
		BranchIsWorktree:     true,
		BranchParentBranch:   "origin/main",
		LastPrompt:           "p",
		LastAssistantMessage: "a",
		LastHookEvent:        "Stop",
		LastHookAt:           now,
	}
	cs.CodexSessionID = "c1"
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-abcd"

	bag := d.Persist(cs)
	got := d.Restore(bag, now.Add(time.Hour)).(CodexState)
	if got.CodexSessionID != "c1" || got.StartDir != "/repo" {
		t.Fatalf("restore mismatch: %+v", got)
	}
	if got.ManagedWorkingDir != "/repo/.roost/worktrees/codex-abcd" || got.WorktreeName != "codex-abcd" {
		t.Fatalf("worktree fields mismatch: %+v", got)
	}
	if got.Status != state.StatusRunning {
		t.Fatalf("Status = %v", got.Status)
	}
	if got.BranchTag != "main" || got.BranchParentBranch != "origin/main" {
		t.Fatalf("branch fields mismatch: %+v", got)
	}
	if got.LastPrompt != "p" || got.LastAssistantMessage != "a" {
		t.Fatalf("message fields mismatch: %+v", got)
	}
}

func TestCodexWarmStartRecoverReinstallsTranscriptWatch(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.TranscriptPath = "/tmp/t.jsonl"
	nextState, effs := d.WarmStartRecover(cs, now)
	next := nextState.(CodexState)
	if next.WatchedFile != "/tmp/t.jsonl" {
		t.Fatalf("WatchedFile = %q, want /tmp/t.jsonl", next.WatchedFile)
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
	if _, ok := job.Input.(CodexTranscriptParseInput); !ok {
		t.Fatalf("job input = %T, want CodexTranscriptParseInput", job.Input)
	}
	if next.Status != cs.Status {
		t.Fatalf("Status = %v, want %v", next.Status, cs.Status)
	}
}

func TestCodexWarmStartRecoverDedupesTranscriptParse(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.TranscriptPath = "/tmp/t.jsonl"
	cs.TranscriptInFlight = true
	nextState, effs := d.WarmStartRecover(cs, now)
	next := nextState.(CodexState)
	if next.WatchedFile != "/tmp/t.jsonl" {
		t.Fatalf("WatchedFile = %q, want /tmp/t.jsonl", next.WatchedFile)
	}
	if len(effs) != 1 {
		t.Fatalf("effects = %d, want 1", len(effs))
	}
	if _, ok := effs[0].(state.EffWatchFile); !ok {
		t.Fatalf("effect = %T, want EffWatchFile", effs[0])
	}
}

func TestCodexTranscriptChangedStartsParse(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.TranscriptPath = "/tmp/t.jsonl"
	next, effs := d.handleTranscriptChanged(cs, state.DEvFileChanged{Path: "/tmp/t.jsonl"})
	if next.WatchedFile != "/tmp/t.jsonl" {
		t.Fatalf("WatchedFile = %q, want /tmp/t.jsonl", next.WatchedFile)
	}
	if !next.TranscriptInFlight {
		t.Fatal("expected TranscriptInFlight")
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
	if _, ok := job.Input.(CodexTranscriptParseInput); !ok {
		t.Fatalf("job input = %T, want CodexTranscriptParseInput", job.Input)
	}
}

func TestCodexTranscriptParseResultMergesFields(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.TranscriptInFlight = true
	next, effs := d.handleJobResult(cs, state.DEvJobResult{
		Now: now,
		Result: CodexTranscriptParseResult{
			Title:                "saved-session",
			LastPrompt:           "Run tests",
			LastAssistantMessage: "done",
			StatusLine:           "gpt-5-codex | 7,205 tok",
			RecentTurns: []SummaryTurn{
				{Role: "user", Text: "Run tests"},
				{Role: "assistant", Text: "done"},
			},
		},
	})
	if next.TranscriptInFlight {
		t.Fatal("TranscriptInFlight should clear")
	}
	if next.Title != "saved-session" || next.LastPrompt != "Run tests" {
		t.Fatalf("unexpected transcript fields: %+v", next)
	}
	if next.LastAssistantMessage != "done" || next.StatusLine != "gpt-5-codex | 7,205 tok" {
		t.Fatalf("unexpected transcript fields: %+v", next)
	}
	if len(next.RecentTurns) != 2 {
		t.Fatalf("RecentTurns len = %d, want 2", len(next.RecentTurns))
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
}

func TestCodexSummaryResultMergesFields(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.SummaryInFlight = true
	next, _ := d.handleJobResult(cs, state.DEvJobResult{
		Now:    now,
		Result: SummaryCommandResult{Summary: "test failures investigation"},
	})
	if next.SummaryInFlight {
		t.Fatal("SummaryInFlight should clear")
	}
	if next.Summary != "test failures investigation" {
		t.Fatalf("Summary = %q", next.Summary)
	}
}

func TestCodexViewAddsTranscriptTab(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.TranscriptPath = "/tmp/t.jsonl"
	cs.Title = "saved-session"
	cs.Summary = "session summary"
	v := d.view(cs)
	if len(v.LogTabs) == 0 {
		t.Fatal("expected tabs")
	}
	if v.LogTabs[0].Label != "TRANSCRIPT" {
		t.Fatalf("first tab = %q", v.LogTabs[0].Label)
	}
	if v.LogTabs[0].Kind != codextranscript.KindTranscript {
		t.Fatalf("tab kind = %q", v.LogTabs[0].Kind)
	}
	if v.Card.Title != "saved-session" {
		t.Fatalf("title = %q", v.Card.Title)
	}
	if v.Card.Subtitle != "session summary" {
		t.Fatalf("subtitle = %q", v.Card.Subtitle)
	}
}

func TestParseCodexWorktree(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantReq worktreeRequest
		wantCmd string
	}{
		{"none", "codex --model gpt-5", worktreeRequest{}, "codex --model gpt-5"},
		{"bare", "codex --worktree", worktreeRequest{Enabled: true}, "codex"},
		{"spaced", "codex --worktree feature --model gpt-5", worktreeRequest{Enabled: true, Name: "feature"}, "codex --model gpt-5"},
		{"equals", "codex --model gpt-5 --worktree=feature", worktreeRequest{Enabled: true, Name: "feature"}, "codex --model gpt-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReq, gotCmd := parseWorktreeFlags(tt.command, "--worktree")
			if gotReq != tt.wantReq || gotCmd != tt.wantCmd {
				t.Fatalf("parseWorktreeFlags(%q) = (%+v, %q), want (%+v, %q)", tt.command, gotReq, gotCmd, tt.wantReq, tt.wantCmd)
			}
		})
	}
}

func TestGeneratedWorktreeNamesLookLikePetnames(t *testing.T) {
	names := generatedWorktreeNames()
	if len(names) != worktreeNameAttempts {
		t.Fatalf("len(names) = %d, want %d", len(names), worktreeNameAttempts)
	}
	for _, name := range names {
		if parts := strings.Split(name, "-"); len(parts) != 4 {
			t.Fatalf("name = %q, want 4 hyphen-separated words", name)
		}
	}
}

func TestCodexPrepareCreateWithoutWorktree(t *testing.T) {
	d, cs, _ := newCodex(t)
	next, plan, err := d.PrepareCreate(cs, "sess-1", "/repo", "codex --model gpt-5", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	if next.(CodexState).WorktreeName != "" {
		t.Fatalf("unexpected worktree state: %+v", next)
	}
	if plan.SetupJob != nil {
		t.Fatal("expected no setup job")
	}
	if plan.Launch.Command != "codex --model gpt-5" || plan.Launch.StartDir != "/repo" {
		t.Fatalf("launch = %+v", plan.Launch)
	}
}

func TestCodexPrepareCreateWithWorktree(t *testing.T) {
	d, cs, _ := newCodex(t)
	next, plan, err := d.PrepareCreate(cs, "sess-1", "/repo", "codex --worktree feature --model gpt-5", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	got := next.(CodexState)
	if got.WorktreeName != "feature" {
		t.Fatalf("WorktreeName = %q", got.WorktreeName)
	}
	if plan.Launch.Command != "codex --model gpt-5" {
		t.Fatalf("launch command = %q", plan.Launch.Command)
	}
	in, ok := plan.SetupJob.(WorktreeSetupInput)
	if !ok {
		t.Fatalf("SetupJob = %T, want WorktreeSetupInput", plan.SetupJob)
	}
	if in.RepoDir != "/repo" || len(in.CandidateNames) != 1 || in.CandidateNames[0] != "feature" {
		t.Fatalf("setup input = %+v", in)
	}
}

func TestCodexCompleteCreateWithWorktree(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.WorktreeName = "feature"
	next, launch, err := d.CompleteCreate(cs, "codex --worktree feature --model gpt-5", state.LaunchOptions{}, WorktreeSetupResult{
		StartDir: "/repo/.roost/worktrees/feature",
		Name:     "feature",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteCreate error: %v", err)
	}
	got := next.(CodexState)
	if got.ManagedWorkingDir != "/repo/.roost/worktrees/feature" || got.StartDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("working dir fields = %+v", got)
	}
	if launch.Command != "codex --worktree feature --model gpt-5" || launch.StartDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("launch = %+v", launch)
	}
}

func TestCodexManagedWorktreePath(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/feature"
	if got := d.ManagedWorktreePath(cs); got != "/repo/.roost/worktrees/feature" {
		t.Fatalf("ManagedWorktreePath = %q", got)
	}
	cs.ManagedWorkingDir = "/repo/feature"
	if got := d.ManagedWorktreePath(cs); got != "" {
		t.Fatalf("ManagedWorktreePath = %q, want empty", got)
	}
}

func TestCodexViewIncludesEventsTab(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.RoostSessionID = "r1"
	cs.BranchTag = "feat-x"
	cs.BranchBG = "#123456"
	cs.BranchFG = "#ffffff"
	v := d.view(cs)
	if len(v.LogTabs) != 1 {
		t.Fatalf("tabs = %d, want 1", len(v.LogTabs))
	}
	if v.LogTabs[0].Label != "EVENTS" {
		t.Fatalf("tab label = %q", v.LogTabs[0].Label)
	}
	if len(v.Card.Tags) != 1 {
		t.Fatalf("tags = %d, want 1", len(v.Card.Tags))
	}
	if v.Card.Tags[0].Text != "feat-x" {
		t.Fatalf("tag text = %q", v.Card.Tags[0].Text)
	}
	if v.Card.Tags[0].Background != "#123456" {
		t.Fatalf("tag bg = %q", v.Card.Tags[0].Background)
	}
	if v.Card.Tags[0].Foreground != "#ffffff" {
		t.Fatalf("tag fg = %q", v.Card.Tags[0].Foreground)
	}
	if v.Card.BorderTitle.Text != CodexDriverName {
		t.Fatalf("border title text = %q", v.Card.BorderTitle.Text)
	}
	if v.Card.BorderTitle.Background != codexTagBg {
		t.Fatalf("border title bg = %q, want %q", v.Card.BorderTitle.Background, codexTagBg)
	}
	if v.Card.BorderTitle.Foreground != codexTagFg {
		t.Fatalf("border title fg = %q, want %q", v.Card.BorderTitle.Foreground, codexTagFg)
	}
}

func TestCodexBranchDetectJobResultUpdatesTag(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.BranchInFlight = true
	next, _, _ := d.Step(cs, state.FrameContext{IsRoot: true}, state.DEvJobResult{
		Now: now,
		Result: BranchDetectResult{
			Branch:       "main",
			Background:   "#222222",
			Foreground:   "#ffffff",
			IsWorktree:   true,
			ParentBranch: "origin/main",
		},
	})
	got := next.(CodexState)
	if got.BranchInFlight {
		t.Fatal("BranchInFlight should be false")
	}
	if got.BranchTag != "main" || got.BranchParentBranch != "origin/main" {
		t.Fatalf("branch state mismatch: %+v", got)
	}
}

func TestCodexCapturePaneOscNotificationsBecomeEffects(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CaptureInFlight = true
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	_, effs := d.handleJobResult(cs, state.DEvJobResult{
		Now: now,
		Result: CapturePaneResult{
			Snapshot: vt.Snapshot{
				Stable:        "hash",
				Notifications: []vt.OscNotification{{Cmd: 9, Payload: "hello"}},
			},
		},
	})
	var notif state.EffRecordNotification
	found := false
	for _, e := range effs {
		if n, ok := e.(state.EffRecordNotification); ok {
			notif = n
			found = true
		}
	}
	if !found {
		t.Fatal("expected EffRecordNotification from OSC 9")
	}
	if notif.Cmd != 9 {
		t.Errorf("Cmd = %d, want 9", notif.Cmd)
	}
	if notif.Title != "hello" {
		t.Errorf("Title = %q, want hello", notif.Title)
	}
}
