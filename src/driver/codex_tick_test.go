package driver

import (
	"errors"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

func TestCodexHandleTickCompletesStartDir(t *testing.T) {
	d := NewCodexDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	cs := d.NewState(now).(CodexState)

	// Before: StartDir is empty
	if cs.StartDir != "" {
		t.Errorf("cs.StartDir = %q, want empty", cs.StartDir)
	}

	// After one tick: StartDir should be filled from e.Project,
	// but BranchDetect should NOT start because status is Idle (Claude-aligned).
	e := state.DEvTick{
		Now:     now.Add(time.Second),
		Active:  true,
		Project: "/repo/project",
	}
	next, effs, _ := d.Step(cs, state.FrameContext{IsRoot: true}, e)
	cs = next.(CodexState)

	if cs.StartDir != "/repo/project" {
		t.Errorf("cs.StartDir = %q, want /repo/project", cs.StartDir)
	}

	for _, eff := range effs {
		if ej, ok := eff.(state.EffStartJob); ok {
			if _, ok := ej.Input.(BranchDetectInput); ok {
				t.Fatal("BranchDetectInput job started on Idle, want skip")
			}
		}
	}

	// Transition to Running
	cs.Status = state.StatusRunning

	// Next tick should now fire BranchDetect
	next, effs, _ = d.Step(cs, state.FrameContext{IsRoot: true}, e)
	cs = next.(CodexState)

	found := false
	for _, eff := range effs {
		if ej, ok := eff.(state.EffStartJob); ok {
			if bdi, ok := ej.Input.(BranchDetectInput); ok {
				if bdi.WorkingDir == "/repo/project" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Error("expected BranchDetectInput job in effects after transition to Running")
	}
}

func TestCodexHangDetection(t *testing.T) {
	d := NewCodexDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	cs := d.NewState(now).(CodexState)
	cs.Status = state.StatusRunning
	cs.StatusChangedAt = now

	// 1. First tick should emit CapturePaneInput
	e := state.DEvTick{Now: now.Add(time.Second), Active: false, PaneTarget: "1"}
	next, effs, _ := d.Step(cs, state.FrameContext{IsRoot: true}, e)
	cs = next.(CodexState)

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

	// 2. Job result primes the baseline
	cs.HandleCapturePaneResult(CapturePaneResult{Snapshot: vt.Snapshot{Stable: "abc"}}, nil, now.Add(2*time.Second))
	if cs.PaneHash != "abc" {
		t.Errorf("PaneHash = %q, want abc", cs.PaneHash)
	}

	// 3. Tick after threshold should trigger Idle
	e.Now = now.Add(commonHangThreshold + 10*time.Second)
	next, _, _ = d.Step(cs, state.FrameContext{IsRoot: true}, e)
	cs = next.(CodexState)

	if cs.Status != state.StatusStopped {
		t.Errorf("Status = %v, want Stopped after hang", cs.Status)
	}
	if !cs.HangDetected {
		t.Error("HangDetected should be true")
	}
}

func TestCodexHandleCapturePaneResultError(t *testing.T) {
	d := NewCodexDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	cs := d.NewState(now).(CodexState)

	// Prime baseline
	cs.HandleCapturePaneResult(CapturePaneResult{Snapshot: vt.Snapshot{Stable: "abc"}}, nil, now)
	at := cs.PaneHashAt

	// Errored capture (zero-value result)
	cs.CaptureInFlight = true
	cs.HandleCapturePaneResult(CapturePaneResult{}, errors.New("tmux failed"), now.Add(10*time.Second))

	// Expect: PaneHash still "abc", PaneHashAt unchanged, CaptureInFlight cleared
	if cs.PaneHash != "abc" {
		t.Errorf("PaneHash = %q, want abc (should not be overwritten on error)", cs.PaneHash)
	}
	if !cs.PaneHashAt.Equal(at) {
		t.Error("PaneHashAt was updated on error")
	}
	if cs.CaptureInFlight {
		t.Error("CaptureInFlight should be cleared even on error")
	}
}

func TestCodexViewIncludesBranchTag(t *testing.T) {
	d := NewCodexDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	cs := d.NewState(now).(CodexState)

	cs.BranchTag = "feat-branch"
	cs.BranchBG = "#abcdef"
	cs.BranchFG = "#000000"

	v := d.View(cs)
	if len(v.Card.Tags) == 0 {
		t.Fatal("expected branch tag in view, but got none")
	}

	found := false
	for _, tag := range v.Card.Tags {
		if tag.Text == "feat-branch" && tag.Background == "#abcdef" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("branch tag not found or mismatched: %+v", v.Card.Tags)
	}
}
