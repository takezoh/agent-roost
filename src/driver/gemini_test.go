package driver

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func newGemini(t *testing.T) (GeminiDriver, GeminiState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	d := NewGeminiDriver("/tmp/events")
	gs := d.NewState(now).(GeminiState)
	return d, gs, now
}

func TestGeminiPrepareCreateWithWorktree(t *testing.T) {
	d, gs, _ := newGemini(t)
	next, plan, err := d.PrepareCreate(gs, "sess-1", "/repo", "gemini --worktree", state.LaunchOptions{})
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	_ = next.(GeminiState)
	in, ok := plan.SetupJob.(WorktreeSetupInput)
	if !ok {
		t.Fatalf("SetupJob = %T", plan.SetupJob)
	}
	if len(in.CandidateNames) != worktreeNameAttempts {
		t.Fatalf("candidate names = %d, want %d", len(in.CandidateNames), worktreeNameAttempts)
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

func TestGeminiManagedWorktreePath(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.WorkingDir = "/repo/.roost/worktrees/example"
	if got := d.ManagedWorktreePath(gs); got == "" {
		t.Fatal("expected managed worktree path")
	}
}
