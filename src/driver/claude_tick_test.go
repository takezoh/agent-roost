package driver

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
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

func TestClaudeHangDetection(t *testing.T) {
	d, cs, now := newClaude(t)
	cs.Status = state.StatusRunning
	cs.StatusChangedAt = now

	// 1. First background tick should emit CapturePaneInput.
	e := state.DEvTick{Now: now.Add(time.Second), Active: false, PaneTarget: "1"}
	next, effs := d.handleTick(cs, e)
	cs = next

	found := false
	for _, eff := range effs {
		if ej, ok := eff.(state.EffStartJob); ok {
			if _, ok := ej.Input.(CapturePaneInput); ok {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected CapturePaneInput on first background tick")
	}

	// 2. Job result primes the baseline.
	cs.HandleCapturePaneResult(CapturePaneResult{Snapshot: vt.Snapshot{Stable: "abc"}}, nil, now.Add(2*time.Second))
	if cs.PaneHash != "abc" {
		t.Errorf("PaneHash = %q, want abc", cs.PaneHash)
	}

	// 3. Tick past the hang threshold should set Stopped.
	e.Now = now.Add(commonHangThreshold + 10*time.Second)
	next, _ = d.handleTick(cs, e)
	cs = next

	if cs.Status != state.StatusStopped {
		t.Errorf("Status = %v, want Stopped after hang", cs.Status)
	}
	if !cs.HangDetected {
		t.Error("HangDetected should be true")
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
