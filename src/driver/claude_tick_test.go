package driver

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestClaudeTickEarlyReturnOnIdle(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusIdle
	cs.StartDir = "/repo" // would normally trigger branch refresh

	next, effs := d.handleTick(cs, state.DEvTick{
		Now:        now.Add(time.Second),
		Active:     true,
		Project:    "/repo",
		PaneTarget: "%5",
	})
	if len(effs) != 0 {
		t.Errorf("Idle handleTick effects = %d, want 0", len(effs))
	}
	if next.Status != state.StatusIdle {
		t.Errorf("Status changed in Idle self-skip: got %v", next.Status)
	}
}

func TestClaudeTickEarlyReturnOnStopped(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusStopped
	cs.StartDir = "/repo"

	_, effs := d.handleTick(cs, state.DEvTick{
		Now:        now.Add(time.Second),
		Active:     true,
		Project:    "/repo",
		PaneTarget: "%5",
	})
	if len(effs) != 0 {
		t.Errorf("Stopped handleTick effects = %d, want 0", len(effs))
	}
}

func TestClaudeTickRunsOnRunning(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	cs.StartDir = "/repo"

	_, effs := d.handleTick(cs, state.DEvTick{
		Now:     now.Add(time.Second),
		Active:  true,
		Project: "/repo",
	})
	// Running + active + non-empty target → branch refresh should fire
	var hasBranch bool
	for _, eff := range effs {
		if job, ok := eff.(state.EffStartJob); ok {
			if _, ok := job.Input.(BranchDetectInput); ok {
				hasBranch = true
			}
		}
	}
	if !hasBranch {
		t.Error("expected BranchDetectInput job for Running+active session")
	}
}
