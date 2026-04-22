package driver

import (
	"errors"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

func TestGeminiHandleTickCompletesStartDir(t *testing.T) {
	d := NewGeminiDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	gs := d.NewState(now).(GeminiState)

	// Before: StartDir is empty
	if gs.StartDir != "" {
		t.Errorf("gs.StartDir = %q, want empty", gs.StartDir)
	}

	// After one tick: StartDir should be filled from e.Project,
	// but BranchDetect should NOT start because status is Idle (Claude-aligned).
	e := state.DEvTick{
		Now:     now.Add(time.Second),
		Active:  true,
		Project: "/repo/project",
	}
	next, effs, _ := d.Step(gs, state.FrameContext{IsRoot: true}, e)
	gs = next.(GeminiState)

	if gs.StartDir != "/repo/project" {
		t.Errorf("gs.StartDir = %q, want /repo/project", gs.StartDir)
	}

	for _, eff := range effs {
		if ej, ok := eff.(state.EffStartJob); ok {
			if _, ok := ej.Input.(BranchDetectInput); ok {
				t.Fatal("BranchDetectInput job started on Idle, want skip")
			}
		}
	}

	// Transition to Running
	gs.Status = state.StatusRunning

	// Next tick should now fire BranchDetect
	next, effs, _ = d.Step(gs, state.FrameContext{IsRoot: true}, e)
	gs = next.(GeminiState)

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

func TestGeminiHangDetection(t *testing.T) {
	d := NewGeminiDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	gs := d.NewState(now).(GeminiState)
	gs.Status = state.StatusRunning
	gs.StatusChangedAt = now

	// 1. First tick should emit CapturePaneInput
	e := state.DEvTick{Now: now.Add(time.Second), Active: false, PaneTarget: "1"}
	next, effs, _ := d.Step(gs, state.FrameContext{IsRoot: true}, e)
	gs = next.(GeminiState)

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
	gs.HandleCapturePaneResult(CapturePaneResult{Snapshot: vt.Snapshot{Stable: "abc"}}, nil, now.Add(2*time.Second))
	if gs.PaneHash != "abc" {
		t.Errorf("PaneHash = %q, want abc", gs.PaneHash)
	}

	// 3. Tick after threshold should trigger Idle
	e.Now = now.Add(commonHangThreshold + 10*time.Second)
	next, _, _ = d.Step(gs, state.FrameContext{IsRoot: true}, e)
	gs = next.(GeminiState)

	if gs.Status != state.StatusStopped {
		t.Errorf("Status = %v, want Stopped after hang", gs.Status)
	}
	if !gs.HangDetected {
		t.Error("HangDetected should be true")
	}
}

func TestGeminiHandleCapturePaneResultError(t *testing.T) {
	d := NewGeminiDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	gs := d.NewState(now).(GeminiState)

	// Prime baseline
	gs.HandleCapturePaneResult(CapturePaneResult{Snapshot: vt.Snapshot{Stable: "abc"}}, nil, now)
	at := gs.PaneHashAt

	// Errored capture (zero-value result)
	gs.CaptureInFlight = true
	gs.HandleCapturePaneResult(CapturePaneResult{}, errors.New("tmux failed"), now.Add(10*time.Second))

	// Expect: PaneHash still "abc", PaneHashAt unchanged
	if gs.PaneHash != "abc" {
		t.Errorf("PaneHash = %q, want abc (should not be overwritten on error)", gs.PaneHash)
	}
	if !gs.PaneHashAt.Equal(at) {
		t.Error("PaneHashAt was updated on error")
	}
	if gs.CaptureInFlight {
		t.Error("CaptureInFlight should be cleared even on error")
	}
}

func TestGeminiViewIncludesBranchTag(t *testing.T) {
	d := NewGeminiDriver("/tmp/events")
	now := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	gs := d.NewState(now).(GeminiState)

	gs.BranchTag = "main"
	gs.BranchBG = "#123456"
	gs.BranchFG = "#ffffff"

	v := d.View(gs)
	if len(v.Card.Tags) == 0 {
		t.Fatal("expected branch tag in view, but got none")
	}

	found := false
	for _, tag := range v.Card.Tags {
		if tag.Text == "main" && tag.Background == "#123456" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("branch tag not found or mismatched: %+v", v.Card.Tags)
	}
}
