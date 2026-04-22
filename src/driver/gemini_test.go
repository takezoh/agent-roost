package driver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

func geminiHook(fields map[string]string, ts time.Time) state.DEvHook {
	raw, _ := json.Marshal(fields)
	return state.DEvHook{Payload: raw, Timestamp: ts}
}

func newGemini(t *testing.T) (GeminiDriver, GeminiState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	d := NewGeminiDriver("/tmp/events")
	gs := d.NewState(now).(GeminiState)
	return d, gs, now
}

func TestGeminiPrepareCreateWithoutWorktree(t *testing.T) {
	d, gs, _ := newGemini(t)
	next, plan, err := d.PrepareCreate(gs, "sess-1", "/repo", "gemini --model flash", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	if next.(GeminiState).WorktreeName != "" {
		t.Fatalf("unexpected worktree state: %+v", next)
	}
	if plan.SetupJob != nil {
		t.Fatal("expected no setup job")
	}
	if plan.Launch.Command != "gemini --model flash" || plan.Launch.StartDir != "/repo" {
		t.Fatalf("launch = %+v", plan.Launch)
	}
}

func TestGeminiPrepareCreateWithWorktree(t *testing.T) {
	d, gs, _ := newGemini(t)
	next, plan, err := d.PrepareCreate(gs, "sess-1", "/repo", "gemini --worktree feature", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	got := next.(GeminiState)
	if got.WorktreeName != "feature" {
		t.Fatalf("WorktreeName = %q", got.WorktreeName)
	}
	if plan.Launch.Command != "gemini" {
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

func TestGeminiPrepareCreateWithWorkspaceAlias(t *testing.T) {
	d, gs, _ := newGemini(t)
	next, plan, err := d.PrepareCreate(gs, "sess-1", "/repo", "gemini --workspace feature", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	got := next.(GeminiState)
	if got.WorktreeName != "feature" {
		t.Fatalf("WorktreeName = %q", got.WorktreeName)
	}
	if _, ok := plan.SetupJob.(WorktreeSetupInput); !ok {
		t.Fatalf("SetupJob = %T, want WorktreeSetupInput", plan.SetupJob)
	}
}

func TestGeminiCompleteCreateWithWorktree(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.WorktreeName = "feature"
	next, launch, err := d.CompleteCreate(gs, "gemini --worktree feature", state.LaunchOptions{}, WorktreeSetupResult{
		StartDir: "/repo/.roost/worktrees/feature",
		Name:     "feature",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteCreate error: %v", err)
	}
	got := next.(GeminiState)
	if got.ManagedWorkingDir != "/repo/.roost/worktrees/feature" || got.StartDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("working dir fields = %+v", got)
	}
	if launch.StartDir != "/repo/.roost/worktrees/feature" {
		t.Fatalf("launch = %+v", launch)
	}
}

func TestGeminiManagedWorktreePath(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.ManagedWorkingDir = "/repo/.roost/worktrees/feature"
	if got := d.ManagedWorktreePath(gs); got != "/repo/.roost/worktrees/feature" {
		t.Fatalf("ManagedWorktreePath = %q", got)
	}
	gs.ManagedWorkingDir = "/repo/feature"
	if got := d.ManagedWorktreePath(gs); got != "" {
		t.Fatalf("ManagedWorktreePath = %q, want empty", got)
	}
}

func TestGeminiPrepareLaunchManagedWorktreeSkipsFlag(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.StartDir = "/repo/.roost/worktrees/feature"
	gs.ManagedWorkingDir = "/repo/.roost/worktrees/feature"
	plan, err := d.PrepareLaunch(gs, state.LaunchModeCreate, "/repo", "gemini", state.LaunchOptions{
		Worktree: state.WorktreeOption{Enabled: true},
	})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	if plan.Command != "gemini" {
		t.Errorf("PrepareLaunch.Command = %q, want %q (no --worktree when managed)", plan.Command, "gemini")
	}
	if plan.StartDir != "/repo/.roost/worktrees/feature" {
		t.Errorf("StartDir = %q", plan.StartDir)
	}
}

func TestGeminiPrepareLaunchWorktreeFromCommand(t *testing.T) {
	d, gs, _ := newGemini(t)
	plan, err := d.PrepareLaunch(gs, state.LaunchModeCreate, "/repo", "gemini --worktree", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	if plan.Command != "gemini --worktree" {
		t.Errorf("PrepareLaunch.Command = %q, want %q", plan.Command, "gemini --worktree")
	}
}

func TestGeminiPrepareLaunchAddsWorktreeFlagFromOptions(t *testing.T) {
	d, gs, _ := newGemini(t)
	plan, err := d.PrepareLaunch(gs, state.LaunchModeCreate, "/repo", "gemini", state.LaunchOptions{
		Worktree: state.WorktreeOption{Enabled: true},
	})
	if err != nil {
		t.Fatalf("PrepareLaunch error: %v", err)
	}
	if got := plan.Command; got != "gemini --worktree" {
		t.Fatalf("PrepareLaunch.Command = %q, want %q", got, "gemini --worktree")
	}
	if plan.Options.Worktree.Enabled {
		t.Fatal("PrepareLaunch.Options.Worktree.Enabled should be false")
	}
}

func TestGeminiSessionStartSetsIdle(t *testing.T) {
	d, gs, now := newGemini(t)
	ev := geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "SessionStart",
		"cwd":             "/repo",
		"transcript_path": "/tmp/t.jsonl",
		"source":          "startup",
	}, now)
	ev.RoostSessionID = "r1"
	next, effs := d.handleHook(gs, state.FrameContext{IsRoot: true}, ev)

	if next.Status != state.StatusIdle {
		t.Fatalf("Status = %v, want idle", next.Status)
	}
	if next.GeminiSessionID != "sess-1" {
		t.Fatalf("GeminiSessionID = %q", next.GeminiSessionID)
	}
	if next.RoostSessionID != "r1" {
		t.Fatalf("RoostSessionID = %q", next.RoostSessionID)
	}
	if next.StartDir != "/repo" || next.TranscriptPath != "/tmp/t.jsonl" {
		t.Fatalf("working data not absorbed: %+v", next)
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

func TestGeminiBeforeAgentTransitionsRunning(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "BeforeAgent",
		"prompt":          "do something",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
	if next.LastPrompt != "do something" {
		t.Fatalf("LastPrompt = %q", next.LastPrompt)
	}
}

func TestGeminiBeforeToolTransitionsRunning(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "BeforeTool",
		"tool_name":       "read_file",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
}

func TestGeminiAfterToolTransitionsRunning(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "AfterTool",
		"tool_name":       "read_file",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
}

func TestGeminiAfterAgentTransitionsWaiting(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	gs.Status = state.StatusRunning
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "AfterAgent",
		"prompt_response": "here is my answer",
	}, now))
	if next.Status != state.StatusWaiting {
		t.Fatalf("Status = %v, want waiting", next.Status)
	}
	if next.LastAssistantMessage != "here is my answer" {
		t.Fatalf("LastAssistantMessage = %q", next.LastAssistantMessage)
	}
}

func TestGeminiNotificationToolPermissionTransitionsPending(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	next, effs := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":        "sess-1",
		"hook_event_name":   "Notification",
		"notification_type": "ToolPermission",
	}, now))
	if next.Status != state.StatusPending {
		t.Fatalf("Status = %v, want pending", next.Status)
	}
	if _, ok := findEffect[state.EffEventLogAppend](effs); !ok {
		t.Fatal("expected EffEventLogAppend")
	}
}

func TestGeminiNotificationUnknownTypeDoesNotChangeStatus(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	gs.Status = state.StatusRunning
	gs.StatusChangedAt = now.Add(-time.Minute)
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":        "sess-1",
		"hook_event_name":   "Notification",
		"notification_type": "something_else",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatalf("Status = %v, want running", next.Status)
	}
}

func TestGeminiSessionEndTransitionsStopped(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	gs.Status = state.StatusWaiting
	next, _ := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "SessionEnd",
	}, now))
	if next.Status != state.StatusStopped {
		t.Fatalf("Status = %v, want stopped", next.Status)
	}
}

func TestGeminiDropsStaleHook(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.GeminiSessionID = "sess-1"
	gs.LastHookAt = now
	next, effs := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"session_id":      "sess-1",
		"hook_event_name": "AfterAgent",
	}, now))
	if next.Status != gs.Status {
		t.Fatal("stale hook should not update status")
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
}

func TestGeminiEmptySessionIDDropped(t *testing.T) {
	d, gs, now := newGemini(t)
	gs.Status = state.StatusRunning
	next, effs := d.handleHook(gs, state.FrameContext{IsRoot: true}, geminiHook(map[string]string{
		"hook_event_name": "AfterAgent",
	}, now))
	if next.Status != state.StatusRunning {
		t.Fatal("empty session_id should not change status")
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
}

func TestGeminiCapturePaneOscNotificationsBecomeEffects(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.CaptureInFlight = true
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	_, effs := d.handleJobResult(gs, state.DEvJobResult{
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
