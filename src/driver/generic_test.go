package driver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func newGenericState(t *testing.T, threshold time.Duration) (GenericDriver, GenericState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	d := NewGenericDriver("bash", threshold)
	s := d.NewState(now).(GenericState)
	return d, s, now
}

func TestGenericNewStateDefaults(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	if s.Name != "bash" {
		t.Errorf("Name = %q, want bash", s.Name)
	}
	if s.Status != state.StatusIdle {
		t.Errorf("Status = %v, want Idle", s.Status)
	}
	if !s.StatusChangedAt.Equal(now) {
		t.Errorf("StatusChangedAt = %v, want %v", s.StatusChangedAt, now)
	}
	if s.IdleThreshold != 5*time.Second {
		t.Errorf("IdleThreshold = %v, want 5s", s.IdleThreshold)
	}
	if s.Primed {
		t.Error("Primed should be false on fresh state")
	}
	_ = d
}

func TestGenericTickEmitsCapturePaneJob(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, WindowTarget: "5"})
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	job, ok := effs[0].(state.EffStartJob)
	if !ok {
		t.Fatalf("effect type = %T, want EffStartJob", effs[0])
	}
	if _, ok := job.Input.(CapturePaneInput); !ok {
		t.Errorf("input type = %T, want CapturePaneInput", job.Input)
	}
	in, ok := job.Input.(CapturePaneInput)
	if !ok {
		t.Fatalf("input type = %T, want CapturePaneInput", job.Input)
	}
	if in.WindowTarget != "5" {
		t.Errorf("WindowTarget = %q, want 5", in.WindowTarget)
	}
}

func TestGenericTickWithoutWindowEmitsNothing(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects when WindowTarget empty, got %d", len(effs))
	}
}

func TestGenericFirstCaptureEstablishesBaseline(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	result := CapturePaneResult{Content: "$ ", Hash: "h1"}
	next, _, _ := d.Step(s, state.DEvJobResult{
		Result: result, Now: now,
	})
	gs := next.(GenericState)
	if !gs.Primed {
		t.Error("Primed should be true after first capture")
	}
	if gs.Hash != "h1" {
		t.Errorf("Hash = %q, want h1", gs.Hash)
	}
	// Status must NOT be touched on first capture (baseline only).
	if gs.Status != state.StatusIdle {
		t.Errorf("Status = %v, want Idle (baseline should not transition)", gs.Status)
	}
}

func TestGenericHashChangeRunningOrWaiting(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Prime first
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "old", Hash: "h0"})

	later := now.Add(2 * time.Second)
	// Hash change without prompt → Running
	next := d.applyCapture(primed, later, CapturePaneResult{Content: "new content", Hash: "h1"})
	if next.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running on hash change without prompt", next.Status)
	}
	if !next.StatusChangedAt.Equal(later) {
		t.Errorf("StatusChangedAt = %v, want %v", next.StatusChangedAt, later)
	}
	if next.Hash != "h1" {
		t.Errorf("Hash = %q, want h1", next.Hash)
	}
}

func TestGenericHashChangeWithPromptIsWaiting(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "old", Hash: "h0"})
	later := now.Add(2 * time.Second)
	next := d.applyCapture(primed, later, CapturePaneResult{Content: "user@host:~$ ", Hash: "h1"})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (prompt detected)", next.Status)
	}
}

func TestGenericIdleThresholdDemotesToIdle(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	// Prime, then transition to Running
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "x", Hash: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, CapturePaneResult{Content: "y", Hash: "h1"})
	if running.Status != state.StatusRunning {
		t.Fatalf("setup failed: status = %v, want Running", running.Status)
	}
	// Same hash, beyond threshold → Idle
	t2 := t1.Add(10 * time.Second)
	idle := d.applyCapture(running, t2, CapturePaneResult{Content: "y", Hash: "h1"})
	if idle.Status != state.StatusIdle {
		t.Errorf("Status = %v, want Idle after threshold", idle.Status)
	}
}

func TestGenericIdleThresholdZeroDisabled(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "x", Hash: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, CapturePaneResult{Content: "y", Hash: "h1"})
	t2 := t1.Add(1 * time.Hour)
	stillRunning := d.applyCapture(running, t2, CapturePaneResult{Content: "y", Hash: "h1"})
	if stillRunning.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running (threshold disabled)", stillRunning.Status)
	}
}

func TestGenericPersistRoundTrip(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	s.Status = state.StatusWaiting
	s.StatusChangedAt = now
	s.Summary = "summary text"
	s.WorkingDir = "/repo/.roost/worktrees/alpha-beta"
	s.WorktreeName = "alpha-beta"
	bag := d.Persist(s)
	if bag[keyStatus] != "waiting" {
		t.Errorf("persisted status = %q, want waiting", bag[keyStatus])
	}
	if bag[keyStatusChangedAt] == "" {
		t.Error("persisted changed_at should not be empty")
	}
	if bag[keySummary] != "summary text" {
		t.Errorf("persisted summary = %q, want summary text", bag[keySummary])
	}
	if bag[keyWorkingDir] != "/repo/.roost/worktrees/alpha-beta" {
		t.Errorf("persisted working dir = %q", bag[keyWorkingDir])
	}
	restored := d.Restore(bag, time.Now()).(GenericState)
	if restored.Status != state.StatusWaiting {
		t.Errorf("restored status = %v, want waiting", restored.Status)
	}
	if !restored.StatusChangedAt.Equal(now) {
		t.Errorf("restored changed_at = %v, want %v", restored.StatusChangedAt, now)
	}
	if restored.Summary != "summary text" {
		t.Errorf("restored summary = %q, want summary text", restored.Summary)
	}
	if restored.WorkingDir != "/repo/.roost/worktrees/alpha-beta" || restored.WorktreeName != "alpha-beta" {
		t.Errorf("restored worktree fields = %+v", restored)
	}
}

func TestGenericPrepareCreateWithWorktree(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	next, plan, err := d.PrepareCreate(s, "sess-1", "/repo", "bash --worktree")
	if err != nil {
		t.Fatalf("PrepareCreate error: %v", err)
	}
	gs := next.(GenericState)
	if gs.WorktreeName == "" {
		t.Fatal("expected generated worktree name")
	}
	in, ok := plan.SetupJob.(WorktreeSetupInput)
	if !ok {
		t.Fatalf("SetupJob = %T", plan.SetupJob)
	}
	if len(in.CandidateNames) != worktreeNameAttempts {
		t.Fatalf("candidate names = %d, want %d", len(in.CandidateNames), worktreeNameAttempts)
	}
}

func TestGenericManagedWorktreePath(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	s.WorkingDir = "/repo/.roost/worktrees/alpha-beta"
	if got := d.ManagedWorktreePath(s); got == "" {
		t.Fatal("expected managed worktree path")
	}
}

func TestGenericRestoreEmptyBag(t *testing.T) {
	d := NewGenericDriver("bash", 5*time.Second)
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	restored := d.Restore(nil, now).(GenericState)
	if restored.Status != state.StatusIdle {
		t.Errorf("empty restore status = %v, want Idle", restored.Status)
	}
	if !restored.StatusChangedAt.Equal(now) {
		t.Errorf("empty restore changed_at = %v, want %v", restored.StatusChangedAt, now)
	}
}

func TestGenericHookEventNoOp(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	next, effs, _ := d.Step(s, state.DEvHook{Event: "session-start"})
	if len(effs) != 0 {
		t.Errorf("hook effects = %d, want 0", len(effs))
	}
	gs := next.(GenericState)
	if gs.Status != state.StatusIdle {
		t.Errorf("Status changed by hook event: %v", gs.Status)
	}
}

func TestGenericViewNoCommandTag(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	v := d.view(s)
	if len(v.Card.Tags) != 0 {
		t.Errorf("tags = %d, want 0", len(v.Card.Tags))
	}
}

func TestGenericViewDisplayName(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	v := d.view(s)
	if v.DisplayName != "bash" {
		t.Errorf("DisplayName = %q, want bash", v.DisplayName)
	}
}

func TestGenericViewBorderTitle(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	v := d.view(s)
	if v.Card.BorderTitle.Text != "bash" {
		t.Errorf("BorderTitle.Text = %q, want bash", v.Card.BorderTitle.Text)
	}
}

func TestGenericWaitingTransitionStartsSummaryJob(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "old", Hash: "h0"})
	next, effs, _ := d.Step(primed, state.DEvJobResult{
		Now: now.Add(time.Second),
		Result: CapturePaneResult{
			Content: "user@host:~$ ",
			Hash:    "h1",
		},
	})
	gs := next.(GenericState)
	if !gs.SummaryInFlight {
		t.Fatal("SummaryInFlight should be true")
	}
	if len(effs) != 1 {
		t.Fatalf("effects = %d, want 1", len(effs))
	}
	job, ok := effs[0].(state.EffStartJob)
	if !ok {
		t.Fatalf("effect type = %T, want EffStartJob", effs[0])
	}
	in, ok := job.Input.(SummaryCommandInput)
	if !ok {
		t.Fatalf("job input type = %T, want SummaryCommandInput", job.Input)
	}
	if !strings.Contains(in.Prompt, "user@host:~$") {
		t.Errorf("Prompt should include trimmed shell prompt: %q", in.Prompt)
	}
}

func TestGenericWaitingTransitionSkipsSummaryWhileInFlight(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.SummaryInFlight = true
	primed := d.applyCapture(s, now, CapturePaneResult{Content: "old", Hash: "h0"})
	next, effs, _ := d.Step(primed, state.DEvJobResult{
		Now: now.Add(time.Second),
		Result: CapturePaneResult{
			Content: "user@host:~$ ",
			Hash:    "h1",
		},
	})
	gs := next.(GenericState)
	if !gs.SummaryInFlight {
		t.Fatal("SummaryInFlight should stay true")
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
}

func TestGenericApplySummaryResult(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.SummaryInFlight = true
	next, effs, _ := d.Step(s, state.DEvJobResult{
		Now:    now,
		Result: SummaryCommandResult{Summary: "new summary"},
	})
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0", len(effs))
	}
	gs := next.(GenericState)
	if gs.SummaryInFlight {
		t.Fatal("SummaryInFlight should be false")
	}
	if gs.Summary != "new summary" {
		t.Errorf("Summary = %q, want new summary", gs.Summary)
	}
}

func TestGenericApplySummaryResultErrorKeepsPrevious(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.SummaryInFlight = true
	s.Summary = "old summary"
	next, _, _ := d.Step(s, state.DEvJobResult{
		Now:    now,
		Result: SummaryCommandResult{Summary: "new summary"},
		Err:    errors.New("failed"),
	})
	gs := next.(GenericState)
	if gs.SummaryInFlight {
		t.Fatal("SummaryInFlight should be false after error")
	}
	if gs.Summary != "old summary" {
		t.Errorf("Summary = %q, want old summary", gs.Summary)
	}
}

func TestGenericViewUsesSummarySubtitle(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	s.Summary = "running tests"
	v := d.view(s)
	if v.Card.Subtitle != "running tests" {
		t.Errorf("Subtitle = %q, want running tests", v.Card.Subtitle)
	}
}

func TestGenericFallbackHasNoBorderTitle(t *testing.T) {
	d := NewGenericDriver("", 0)
	s := d.NewState(time.Now()).(GenericState)
	v := d.view(s)
	if v.Card.BorderTitle.Text != "" {
		t.Errorf("fallback BorderTitle.Text = %q, want empty", v.Card.BorderTitle.Text)
	}
}

func TestGenericFallbackHasNoCommandTag(t *testing.T) {
	d := NewGenericDriver("", 0)
	s := d.NewState(time.Now()).(GenericState)
	v := d.view(s)
	if len(v.Card.Tags) != 0 {
		t.Errorf("fallback tags = %d, want 0", len(v.Card.Tags))
	}
	if v.DisplayName != "" {
		t.Errorf("fallback DisplayName = %q, want empty", v.DisplayName)
	}
}

func TestGenericRegisteredViaInit(t *testing.T) {
	// Phase 2-A only registers the fallback. Phase 2-B will register
	// other named drivers and fallback driver. For now we just verify
	// the fallback resolves.
	d := state.GetDriver("/usr/bin/bash")
	if d == nil {
		t.Skip("no driver registered yet (Phase 2-B will register them)")
	}
}

func TestWithDisplayName(t *testing.T) {
	d := NewGenericDriver("shell", 0).WithDisplayName("zsh")
	if d.Name() != "shell" {
		t.Errorf("Name() = %q, want shell", d.Name())
	}
	if d.DisplayName() != "zsh" {
		t.Errorf("DisplayName() = %q, want zsh", d.DisplayName())
	}
	s := d.NewState(time.Now()).(GenericState)
	v := d.view(s)
	if len(v.Card.Tags) != 0 {
		t.Errorf("tags = %d, want 0", len(v.Card.Tags))
	}
	if v.DisplayName != "zsh" {
		t.Errorf("DisplayName = %q, want zsh", v.DisplayName)
	}
	if v.Card.BorderTitle.Text != "zsh" {
		t.Errorf("BorderTitle = %q, want zsh", v.Card.BorderTitle.Text)
	}
}

func TestHashContentDeterministic(t *testing.T) {
	a := hashContent("hello")
	b := hashContent("hello")
	c := hashContent("world")
	if a != b {
		t.Error("hashContent not deterministic")
	}
	if a == c {
		t.Error("hashContent collision on different inputs")
	}
}
