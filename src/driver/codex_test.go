package driver

import (
	"encoding/json"
	"testing"
	"time"

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
	next, effs := d.handleHook(cs, ev)

	if next.Status != state.StatusIdle {
		t.Fatalf("Status = %v, want idle", next.Status)
	}
	if next.CodexSessionID != "sess-1" {
		t.Fatalf("CodexSessionID = %q", next.CodexSessionID)
	}
	if next.RoostSessionID != "r1" {
		t.Fatalf("RoostSessionID = %q", next.RoostSessionID)
	}
	if next.WorkingDir != "/repo" || next.TranscriptPath != "/tmp/t.jsonl" {
		t.Fatalf("working data not absorbed: %+v", next)
	}
	if len(effs) != 2 {
		t.Fatalf("effects = %d, want 2", len(effs))
	}
	foundBranch := false
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			foundBranch = true
		}
	}
	if !foundBranch {
		t.Fatal("expected BranchDetectInput job")
	}
}

func TestCodexUserPromptTransitionsRunning(t *testing.T) {
	d, cs, now := newCodex(t)
	next, _ := d.handleHook(cs, codexHook(map[string]string{
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
}

func TestCodexStopTransitionsWaiting(t *testing.T) {
	d, cs, now := newCodex(t)
	next, _ := d.handleHook(cs, codexHook(map[string]string{
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

func TestCodexDropsStaleHook(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.LastHookAt = now
	next, effs := d.handleHook(cs, codexHook(map[string]string{
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

func TestCodexSpawnCommandResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	got := d.SpawnCommand(cs, "codex --model gpt-5-codex")
	want := "codex --model gpt-5-codex resume abc-123"
	if got != want {
		t.Fatalf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestCodexSpawnCommandNoDoubleResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	got := d.SpawnCommand(cs, "codex resume abc")
	if got != "codex resume abc" {
		t.Fatalf("SpawnCommand = %q", got)
	}
}

func TestCodexSpawnCommandStripsWorktreeOnResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-1234"
	got := d.SpawnCommand(cs, "codex --worktree feature --model gpt-5-codex")
	want := "codex --model gpt-5-codex resume abc-123"
	if got != want {
		t.Fatalf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestCodexSpawnCommandStripsWorktreeWithoutResume(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-1234"
	got := d.SpawnCommand(cs, "codex --worktree feature --model gpt-5-codex")
	want := "codex --model gpt-5-codex"
	if got != want {
		t.Fatalf("SpawnCommand = %q, want %q", got, want)
	}
}

func TestCodexSpawnCommandSkipsNonCodexBaseCommand(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.CodexSessionID = "abc-123"
	got := d.SpawnCommand(cs, "env FOO=bar")
	if got != "env FOO=bar" {
		t.Fatalf("SpawnCommand = %q", got)
	}
}

func TestCodexPersistRestoreRoundTrip(t *testing.T) {
	d, cs, now := newCodex(t)
	cs.RoostSessionID = "r1"
	cs.CodexSessionID = "c1"
	cs.WorkingDir = "/repo"
	cs.ManagedWorkingDir = "/repo/.roost/worktrees/codex-abcd"
	cs.TranscriptPath = "/repo/t.jsonl"
	cs.WorktreeName = "codex-abcd"
	cs.Status = state.StatusRunning
	cs.StatusChangedAt = now
	cs.BranchTag = "main"
	cs.BranchBG = "#111111"
	cs.BranchFG = "#ffffff"
	cs.BranchTarget = "/repo"
	cs.BranchAt = now
	cs.BranchIsWorktree = true
	cs.BranchParentBranch = "origin/main"
	cs.LastPrompt = "p"
	cs.LastAssistantMessage = "a"
	cs.LastHookEvent = "Stop"
	cs.LastHookAt = now

	bag := d.Persist(cs)
	got := d.Restore(bag, now.Add(time.Hour)).(CodexState)
	if got.CodexSessionID != "c1" || got.WorkingDir != "/repo" {
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

func TestParseCodexWorktree(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantReq codexWorktreeRequest
		wantCmd string
	}{
		{"none", "codex --model gpt-5", codexWorktreeRequest{}, "codex --model gpt-5"},
		{"bare", "codex --worktree", codexWorktreeRequest{Enabled: true}, "codex"},
		{"spaced", "codex --worktree feature --model gpt-5", codexWorktreeRequest{Enabled: true, Name: "feature"}, "codex --model gpt-5"},
		{"equals", "codex --model gpt-5 --worktree=feature", codexWorktreeRequest{Enabled: true, Name: "feature"}, "codex --model gpt-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotReq, gotCmd := parseCodexWorktree(tt.command)
			if gotReq != tt.wantReq || gotCmd != tt.wantCmd {
				t.Fatalf("parseCodexWorktree(%q) = (%+v, %q), want (%+v, %q)", tt.command, gotReq, gotCmd, tt.wantReq, tt.wantCmd)
			}
		})
	}
}

func TestCodexPrepareCreateWithoutWorktree(t *testing.T) {
	d, cs, _ := newCodex(t)
	next, plan, err := d.PrepareCreate(cs, "sess-1", "/repo", "codex --model gpt-5")
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
	next, plan, err := d.PrepareCreate(cs, "sess-1", "/repo", "codex --worktree feature --model gpt-5")
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
	if in.RepoDir != "/repo" || in.Name != "feature" {
		t.Fatalf("setup input = %+v", in)
	}
}

func TestCodexCompleteCreateWithWorktree(t *testing.T) {
	d, cs, _ := newCodex(t)
	cs.WorktreeName = "feature"
	next, launch, err := d.CompleteCreate(cs, "codex --worktree feature --model gpt-5", WorktreeSetupResult{
		WorkingDir: "/repo/.roost/worktrees/feature",
		Name:       "feature",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteCreate error: %v", err)
	}
	got := next.(CodexState)
	if got.ManagedWorkingDir != "/repo/.roost/worktrees/feature" || got.WorkingDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("working dir fields = %+v", got)
	}
	if launch.Command != "codex --model gpt-5" || launch.StartDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("launch = %+v", launch)
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
	next, _, _ := d.Step(cs, state.DEvJobResult{
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
