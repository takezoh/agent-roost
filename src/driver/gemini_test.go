package driver

import (
	"testing"
	"time"
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
	next, plan, err := d.PrepareCreate(gs, "sess-1", "/repo", "gemini --worktree")
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

func TestGeminiManagedWorktreePath(t *testing.T) {
	d, gs, _ := newGemini(t)
	gs.WorkingDir = "/repo/.roost/worktrees/example"
	if got := d.ManagedWorktreePath(gs); got == "" {
		t.Fatal("expected managed worktree path")
	}
}
