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

func TestGeminiNotCreateSessionPlanner(t *testing.T) {
	d, _, _ := newGemini(t)
	if _, ok := any(d).(state.CreateSessionPlanner); ok {
		t.Fatal("GeminiDriver must not implement CreateSessionPlanner (worktree handled by gemini CLI itself)")
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

