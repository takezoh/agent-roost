package driver

import (
	"sync/atomic"
	"testing"
	"time"
)

func timeZero() time.Time { return time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC) }

// newClaudeWithStubBranch builds a claudeDriver and replaces the default
// git.DetectBranch with a counting stub. Tests use this to assert how often
// branch detection runs without actually forking git.
func newClaudeWithStubBranch(t *testing.T, ctx SessionContext, fn func(string) string) (*claudeDriver, *int32) {
	t.Helper()
	d := newClaudeFactory()(Deps{Session: ctx}).(*claudeDriver)
	var calls int32
	d.detectBranch = func(dir string) string {
		atomic.AddInt32(&calls, 1)
		return fn(dir)
	}
	return d, &calls
}

func TestRefreshBranch_RunsOnFirstCall(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "s"}
	d, calls := newClaudeWithStubBranch(t, ctx, func(string) string { return "main" })

	d.refreshBranch(timeZero(), "/proj")

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("DetectBranch calls = %d, want 1", got)
	}
	if d.branchTag != "main" {
		t.Errorf("branchTag = %q, want main", d.branchTag)
	}
}

func TestRefreshBranch_SkipsBeforeIntervalElapses(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "s"}
	d, calls := newClaudeWithStubBranch(t, ctx, func(string) string { return "main" })

	t0 := timeZero()
	d.refreshBranch(t0, "/proj")
	d.refreshBranch(t0.Add(5*time.Second), "/proj")
	d.refreshBranch(t0.Add(20*time.Second), "/proj")

	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("DetectBranch calls = %d, want 1 (cache hit within interval)", got)
	}
}

func TestRefreshBranch_ReRunsAfterInterval(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "s"}
	d, calls := newClaudeWithStubBranch(t, ctx, func(string) string { return "main" })

	t0 := timeZero()
	d.refreshBranch(t0, "/proj")
	d.refreshBranch(t0.Add(claudeBranchRefreshInterval+time.Second), "/proj")

	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("DetectBranch calls = %d, want 2 (re-run after interval)", got)
	}
}

func TestRefreshBranch_TargetChangeForcesImmediateReDetect(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "s"}
	d, calls := newClaudeWithStubBranch(t, ctx, func(string) string { return "main" })

	t0 := timeZero()
	d.refreshBranch(t0, "/proj")
	// Same time, different target → should re-detect immediately.
	d.refreshBranch(t0, "/other-proj")

	if got := atomic.LoadInt32(calls); got != 2 {
		t.Errorf("DetectBranch calls = %d, want 2 (target change forces re-detect)", got)
	}
}

func TestRefreshBranch_PrefersWorkingDirOverFallback(t *testing.T) {
	var seen string
	ctx := &fakeSessionContext{active: true, id: "s"}
	d := newClaudeFactory()(Deps{Session: ctx}).(*claudeDriver)
	d.detectBranch = func(dir string) string {
		seen = dir
		return ""
	}

	d.mu.Lock()
	d.workingDir = "/agent/cwd"
	d.mu.Unlock()
	d.refreshBranch(timeZero(), "/project/root")

	if seen != "/agent/cwd" {
		t.Errorf("DetectBranch called with %q, want /agent/cwd (workingDir takes priority)", seen)
	}
}

func TestRefreshBranch_NilDetectorIsNoop(t *testing.T) {
	ctx := &fakeSessionContext{active: true, id: "s"}
	d := newClaudeFactory()(Deps{Session: ctx}).(*claudeDriver)
	d.detectBranch = nil
	// Must not panic.
	d.refreshBranch(timeZero(), "/proj")
	if d.branchTag != "" {
		t.Errorf("branchTag = %q, want empty", d.branchTag)
	}
}

func TestClaudeDriver_TickGatesBranchOnActive(t *testing.T) {
	ctx := &fakeSessionContext{active: false, id: "s"}
	d, calls := newClaudeWithStubBranch(t, ctx, func(string) string { return "main" })

	// Inactive: Tick must not call DetectBranch.
	for i := 0; i < 5; i++ {
		d.Tick(timeZero(), nil)
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Errorf("inactive Tick: DetectBranch calls = %d, want 0", got)
	}

	// Activate and tick once: branch should be detected.
	ctx.active = true
	d.mu.Lock()
	d.workingDir = "/proj"
	d.mu.Unlock()
	d.Tick(timeZero(), nil)
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("active Tick: DetectBranch calls = %d, want 1", got)
	}
}
