package driver

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

func newGenericState(t *testing.T, threshold time.Duration) (GenericDriver, GenericState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	d := NewGenericDriver("bash", "bash", threshold)
	s := d.NewState(now).(GenericState)
	return d, s, now
}

func TestGenericNewStateDefaults(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	if s.Name != "bash" {
		t.Errorf("Name = %q, want bash", s.Name)
	}
	if s.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting", s.Status)
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

// Active sessions receive capture-pane via DEvPaneActivity (tap-driven), not tick.
func TestGenericTickActiveDoesNotEmitCapturePaneJob(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, PaneTarget: "5"})
	for _, eff := range effs {
		if job, ok := eff.(state.EffStartJob); ok {
			if _, ok := job.Input.(CapturePaneInput); ok {
				t.Error("active session tick must not emit capture-pane (handled by DEvPaneActivity)")
			}
		}
	}
}

func TestGenericActivityEmitsCapturePaneJob(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvPaneActivity{PaneTarget: "5", Now: time.Now()})
	if len(effs) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effs))
	}
	job, ok := effs[0].(state.EffStartJob)
	if !ok {
		t.Fatalf("effect type = %T, want EffStartJob", effs[0])
	}
	in, ok := job.Input.(CapturePaneInput)
	if !ok {
		t.Errorf("input type = %T, want CapturePaneInput", job.Input)
	}
	if in.PaneTarget != "5" {
		t.Errorf("PaneTarget = %q, want 5", in.PaneTarget)
	}
}

func TestGenericTickWithoutWindowEmitsNothing(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Default state is Waiting; active=true (no Active set but PaneTarget empty).
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects when PaneTarget empty, got %d", len(effs))
	}
}

func TestGenericTickSkipsWhenParkedAndWaiting(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Status=Waiting (default), Active=false → self-gate skips tick
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: false, PaneTarget: "5"})
	if len(effs) != 0 {
		t.Errorf("expected 0 effects for parked+waiting, got %d", len(effs))
	}
}

func TestGenericTickRunsWhenActive(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Status=Waiting but Active=true → tick is processed (branch detect may run)
	// Capture-pane is no longer issued from tick for active sessions; DEvPaneActivity handles it.
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, PaneTarget: "5"})
	for _, eff := range effs {
		if job, ok := eff.(state.EffStartJob); ok {
			if _, ok := job.Input.(CapturePaneInput); ok {
				t.Error("active session tick must not emit capture-pane job")
			}
		}
	}
}

func TestGenericTickRunsWhenParkedAndRunning(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Bring to Running via hash change (prime → hash change)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	running := d.applyCapture(primed, now.Add(time.Second), vt.Snapshot{Stable: "h1"})
	if running.Status != state.StatusRunning {
		t.Fatalf("setup: status = %v, want Running", running.Status)
	}
	// Parked but Running → tick is processed
	_, effs, _ := d.Step(running, state.DEvTick{Now: now.Add(2 * time.Second), Active: false, PaneTarget: "5"})
	var hasCapture bool
	for _, eff := range effs {
		if job, ok := eff.(state.EffStartJob); ok {
			if _, ok := job.Input.(CapturePaneInput); ok {
				hasCapture = true
			}
		}
	}
	if !hasCapture {
		t.Error("expected capture-pane job when parked+running")
	}
}

func TestGenericTickRunsBranchRefreshOnlyWhenActive(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Parked but Running → capture fires but branch detect does NOT
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	running := d.applyCapture(primed, now.Add(time.Second), vt.Snapshot{Stable: "h1"})
	_, effs, _ := d.Step(running, state.DEvTick{Now: now.Add(2 * time.Second), Active: false, PaneTarget: "5", Project: "/repo"})
	for _, eff := range effs {
		if job, ok := eff.(state.EffStartJob); ok {
			if _, ok := job.Input.(BranchDetectInput); ok {
				t.Error("branch detect should not fire when parked (Active=false)")
			}
		}
	}
}

func TestGenericFirstCaptureEstablishesBaseline(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	result := CapturePaneResult{Content: "$ ", Snapshot: vt.Snapshot{Stable: "h1"}}
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
	if gs.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (baseline should not transition)", gs.Status)
	}
}

func TestGenericHashChangeEntersRunning(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Prime first (Status stays Waiting)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	if primed.Status != state.StatusWaiting {
		t.Fatalf("setup: status = %v, want Waiting after baseline", primed.Status)
	}

	later := now.Add(2 * time.Second)
	// Hash change from Waiting → Running; StatusChangedAt must be updated
	next := d.applyCapture(primed, later, vt.Snapshot{Stable: "h1"})
	if next.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running on hash change from Waiting", next.Status)
	}
	if !next.StatusChangedAt.Equal(later) {
		t.Errorf("StatusChangedAt = %v, want %v (transition Waiting→Running)", next.StatusChangedAt, later)
	}
	if next.Hash != "h1" {
		t.Errorf("Hash = %q, want h1", next.Hash)
	}
	if !next.LastActivity.Equal(later) {
		t.Errorf("LastActivity = %v, want %v", next.LastActivity, later)
	}
}

func TestGenericStabilityThresholdEntersWaiting(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	// Prime baseline
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	// Hash change → Running, LastActivity = t1
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	if running.Status != state.StatusRunning {
		t.Fatalf("setup failed: status = %v, want Running", running.Status)
	}
	// Same hash, beyond threshold → Waiting
	t2 := t1.Add(6 * time.Second)
	waiting := d.applyCapture(running, t2, vt.Snapshot{Stable: "h1"})
	if waiting.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting after stability threshold", waiting.Status)
	}
	if !waiting.StatusChangedAt.Equal(t2) {
		t.Errorf("StatusChangedAt = %v, want %v", waiting.StatusChangedAt, t2)
	}
}

func TestGenericStabilityThresholdPreservesRunningWhenBelow(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	// Same hash, below threshold → still Running
	t2 := t1.Add(3 * time.Second)
	still := d.applyCapture(running, t2, vt.Snapshot{Stable: "h1"})
	if still.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running (below threshold)", still.Status)
	}
}

func TestGenericStabilityThresholdZeroDisabled(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	t2 := t1.Add(1 * time.Hour)
	stillRunning := d.applyCapture(running, t2, vt.Snapshot{Stable: "h1"})
	if stillRunning.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running (threshold disabled)", stillRunning.Status)
	}
}

func TestGenericWaitingToRunningOnHashChange(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	// Bring to Waiting via stability threshold
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	t2 := t1.Add(10 * time.Second)
	waiting := d.applyCapture(running, t2, vt.Snapshot{Stable: "h1"})
	if waiting.Status != state.StatusWaiting {
		t.Fatalf("setup failed: status = %v, want Waiting", waiting.Status)
	}
	// Hash change from Waiting → Running, StatusChangedAt updated
	t3 := t2.Add(2 * time.Second)
	resumed := d.applyCapture(waiting, t3, vt.Snapshot{Stable: "h2"})
	if resumed.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running after hash change from Waiting", resumed.Status)
	}
	if !resumed.StatusChangedAt.Equal(t3) {
		t.Errorf("StatusChangedAt = %v, want %v", resumed.StatusChangedAt, t3)
	}
}

func TestGenericRunningConsecutiveDoesNotResetChangedAt(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	// Prime baseline (Waiting)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	// First hash change: Waiting → Running; StatusChangedAt = t1
	r1 := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	if r1.Status != state.StatusRunning {
		t.Fatalf("setup: status = %v, want Running after first hash change", r1.Status)
	}
	origChangedAt := r1.StatusChangedAt
	// Second hash change while already Running: StatusChangedAt must NOT be updated
	t2 := t1.Add(time.Second)
	r2 := d.applyCapture(r1, t2, vt.Snapshot{Stable: "h2"})
	if r2.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running", r2.Status)
	}
	if !r2.StatusChangedAt.Equal(origChangedAt) {
		t.Errorf("StatusChangedAt reset on consecutive Running: got %v, want %v", r2.StatusChangedAt, origChangedAt)
	}
}

// TestGenericPromptEntersWaiting was removed: GenericDriver no longer applies
// the promptRe heuristic. Shell-prompt detection now lives in ShellDriver
// (see shell_test.go).

func TestGenericPersistRoundTrip(t *testing.T) {
	d, s, now := newGenericState(t, 5*time.Second)
	s.Status = state.StatusWaiting
	s.StatusChangedAt = now
	s.Summary = "summary text"
	s.StartDir = "/repo/.roost/worktrees/alpha-beta"
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
	if bag[keyStartDir] != "/repo/.roost/worktrees/alpha-beta" {
		t.Errorf("persisted working dir = %q", bag[keyStartDir])
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
	if restored.StartDir != "/repo/.roost/worktrees/alpha-beta" || restored.WorktreeName != "alpha-beta" {
		t.Errorf("restored worktree fields = %+v", restored)
	}
}

func TestGenericPrepareCreateWithWorktree(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	next, plan, err := d.PrepareCreate(s, "sess-1", "/repo", "bash --worktree", state.LaunchOptions{})
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
	s.StartDir = "/repo/.roost/worktrees/alpha-beta"
	if got := d.ManagedWorktreePath(s); got == "" {
		t.Fatal("expected managed worktree path")
	}
}

func TestGenericRestoreEmptyBag(t *testing.T) {
	d := NewGenericDriver("bash", "bash", 5*time.Second)
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	restored := d.Restore(nil, now).(GenericState)
	if restored.Status != state.StatusWaiting {
		t.Errorf("empty restore status = %v, want Waiting", restored.Status)
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
	if gs.Status != state.StatusWaiting {
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
	const threshold = 5 * time.Second
	d, s, now := newGenericState(t, threshold)
	// 1st capture: prime baseline
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	// 2nd capture: hash changes → still Running
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	if running.Status != state.StatusRunning {
		t.Fatalf("setup: status = %v, want Running", running.Status)
	}
	// 3rd capture via Step: same hash, beyond threshold → Waiting + summary job
	t2 := t1.Add(threshold + time.Second)
	next, effs, _ := d.Step(running, state.DEvJobResult{
		Now: t2,
		Result: CapturePaneResult{
			Content:  "build done",
			Snapshot: vt.Snapshot{Stable: "h1"},
		},
	})
	gs := next.(GenericState)
	if gs.Status != state.StatusWaiting {
		t.Fatalf("Status = %v, want Waiting after stability threshold", gs.Status)
	}
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
	if !strings.Contains(in.Prompt, "terminal session summarizer") {
		t.Errorf("Prompt should contain 'terminal session summarizer': %q", in.Prompt)
	}
	if strings.Contains(in.Prompt, "AI coding session") {
		t.Errorf("Prompt should not contain agent-specific wording: %q", in.Prompt)
	}
}

func TestGenericWaitingTransitionSkipsSummaryWhileInFlight(t *testing.T) {
	const threshold = 5 * time.Second
	d, s, now := newGenericState(t, threshold)
	s.SummaryInFlight = true
	// Prime, then move to Running
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	// Stability threshold exceeded via Step → Waiting, but SummaryInFlight already true
	t2 := t1.Add(threshold + time.Second)
	next, effs, _ := d.Step(running, state.DEvJobResult{
		Now: t2,
		Result: CapturePaneResult{
			Content:  "y",
			Snapshot: vt.Snapshot{Stable: "h1"},
		},
	})
	gs := next.(GenericState)
	if !gs.SummaryInFlight {
		t.Fatal("SummaryInFlight should stay true")
	}
	if len(effs) != 0 {
		t.Fatalf("effects = %d, want 0 (already in-flight)", len(effs))
	}
}

func TestGenericSummaryPromptIsGenericFormat(t *testing.T) {
	const threshold = 5 * time.Second
	d, s, now := newGenericState(t, threshold)
	primed := d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
	t1 := now.Add(time.Second)
	running := d.applyCapture(primed, t1, vt.Snapshot{Stable: "h1"})
	t2 := t1.Add(threshold + time.Second)
	_, effs, _ := d.Step(running, state.DEvJobResult{
		Now: t2,
		Result: CapturePaneResult{
			Content:  "diff content changed",
			Snapshot: vt.Snapshot{Stable: "h1"},
		},
	})
	if len(effs) != 1 {
		t.Fatalf("effects = %d, want 1", len(effs))
	}
	in, ok := effs[0].(state.EffStartJob).Input.(SummaryCommandInput)
	if !ok {
		t.Fatalf("job input type unexpected")
	}
	if !strings.Contains(in.Prompt, "terminal session summarizer") {
		t.Errorf("Prompt missing 'terminal session summarizer'")
	}
	if !strings.Contains(in.Prompt, "<terminal_output>") {
		t.Errorf("Prompt missing <terminal_output> tag")
	}
	if !strings.Contains(in.Prompt, "<command>\nbash\n</command>") {
		t.Errorf("Prompt missing command annotation (displayName=bash)")
	}
	for _, bad := range []string{"AI coding session", "user turn", "recent_turns"} {
		if strings.Contains(in.Prompt, bad) {
			t.Errorf("Prompt contains agent-specific wording %q", bad)
		}
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
	d := NewGenericDriver("", "", 0)
	s := d.NewState(time.Now()).(GenericState)
	v := d.view(s)
	if v.Card.BorderTitle.Text != "" {
		t.Errorf("fallback BorderTitle.Text = %q, want empty", v.Card.BorderTitle.Text)
	}
}

func TestGenericFallbackHasNoCommandTag(t *testing.T) {
	d := NewGenericDriver("", "", 0)
	s := d.NewState(time.Now()).(GenericState)
	v := d.view(s)
	if len(v.Card.Tags) != 0 {
		t.Errorf("fallback tags = %d, want 0", len(v.Card.Tags))
	}
	if v.DisplayName != "" {
		t.Errorf("fallback DisplayName = %q, want empty", v.DisplayName)
	}
}

func TestGetDriverFallbackFactory(t *testing.T) {
	state.ClearRegistry()
	state.RegisterFallbackFactory(func(command string) state.Driver {
		name := state.FirstToken(command)
		return NewGenericDriver(name, name, 0)
	})

	// "tig status" のような未知のコマンドに対してフォールバックファクトリが呼ばれることを確認
	d := state.GetDriver("tig status")
	if d.Name() != "tig" {
		t.Errorf("Driver Name = %q, want tig", d.Name())
	}
	if d.DisplayName() != "tig" {
		t.Errorf("Driver DisplayName = %q, want tig", d.DisplayName())
	}

	// 登録済みのドライバはフォールバックファクトリが呼ばれないことを確認
	state.Register(NewGenericDriver("mycmd", "My Command", 0))
	d2 := state.GetDriver("mycmd args")
	if d2.Name() != "mycmd" {
		t.Errorf("Registered Driver Name = %q, want mycmd", d2.Name())
	}
	if d2.DisplayName() != "My Command" {
		t.Errorf("Registered Driver DisplayName = %q, want My Command", d2.DisplayName())
	}
}

func TestGenericViewFallbackChip(t *testing.T) {
	state.ClearRegistry()
	state.RegisterFallbackFactory(func(command string) state.Driver {
		name := state.FirstToken(command)
		return NewGenericDriver(name, name, 0)
	})

	d := state.GetDriver("tig status")
	s := d.NewState(time.Now())
	v := d.View(s) // View() メソッドは Driver インターフェースにある
	if v.Card.BorderTitle.Text != "tig" {
		t.Errorf("Fallback Driver View BorderTitle = %q, want tig", v.Card.BorderTitle.Text)
	}
	if v.DisplayName != "tig" {
		t.Errorf("Fallback Driver View DisplayName = %q, want tig", v.DisplayName)
	}
}

// ---- Branch detection tests ----

func TestGenericTickActiveSchedulesBranchJob(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo"})
	var found bool
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if in, ok := job.Input.(BranchDetectInput); ok && in.WorkingDir == "/repo" {
			found = true
		}
	}
	if !found {
		t.Error("expected BranchDetectInput job when active with project")
	}
}

func TestGenericTickActiveSchedulesBranchJobUpdatesState(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	next, _, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo"})
	gs := next.(GenericState)
	if !gs.BranchInFlight {
		t.Error("BranchInFlight should be true after branch job scheduled")
	}
	if gs.BranchTarget != "/repo" {
		t.Errorf("BranchTarget = %q, want /repo", gs.BranchTarget)
	}
}

func TestGenericTickActiveWithPaneEmitsBranch(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo", PaneTarget: "5"})
	var hasBranch bool
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			hasBranch = true
		}
		if _, ok := job.Input.(CapturePaneInput); ok {
			t.Error("active session tick must not emit capture-pane (handled by DEvPaneActivity)")
		}
	}
	if !hasBranch {
		t.Error("expected BranchDetectInput job")
	}
}

func TestGenericTickInactiveSkipsBranchDetect(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: false, Project: "/repo", PaneTarget: "5"})
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			t.Error("branch detect should not be scheduled when inactive")
		}
	}
}

func TestGenericTickFreshCacheSkipsBranchDetect(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchTarget = "/repo"
	s.BranchAt = now.Add(-10 * time.Second) // within 30s
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo"})
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			t.Error("branch detect should be skipped when cache is fresh")
		}
	}
}

func TestGenericTickStaleCacheRefreshesBranchDetect(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchTarget = "/repo"
	s.BranchAt = now.Add(-31 * time.Second) // stale
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo"})
	var found bool
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected branch detect when cache is stale")
	}
}

func TestGenericTickBranchInFlightSkips(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchInFlight = true
	_, effs, _ := d.Step(s, state.DEvTick{Now: now, Active: true, Project: "/repo"})
	for _, eff := range effs {
		job, ok := eff.(state.EffStartJob)
		if !ok {
			continue
		}
		if _, ok := job.Input.(BranchDetectInput); ok {
			t.Error("branch detect should be skipped when in-flight")
		}
	}
}

func TestGenericBranchDetectResultUpdatesTag(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchInFlight = true
	ev := state.DEvJobResult{
		Now: now,
		Result: BranchDetectResult{
			Branch:       "feature/x",
			Background:   "#aaa",
			Foreground:   "#fff",
			IsWorktree:   true,
			ParentBranch: "main",
		},
	}
	next, _, _ := d.Step(s, ev)
	gs := next.(GenericState)
	if gs.BranchInFlight {
		t.Error("BranchInFlight should be false after result")
	}
	if gs.BranchTag != "feature/x" {
		t.Errorf("BranchTag = %q, want feature/x", gs.BranchTag)
	}
	if gs.BranchBG != "#aaa" {
		t.Errorf("BranchBG = %q, want #aaa", gs.BranchBG)
	}
	if gs.BranchFG != "#fff" {
		t.Errorf("BranchFG = %q, want #fff", gs.BranchFG)
	}
	if !gs.BranchIsWorktree {
		t.Error("BranchIsWorktree should be true")
	}
	if gs.BranchParentBranch != "main" {
		t.Errorf("BranchParentBranch = %q, want main", gs.BranchParentBranch)
	}
	if !gs.BranchAt.Equal(now) {
		t.Errorf("BranchAt = %v, want %v", gs.BranchAt, now)
	}
}

func TestGenericBranchDetectResultEmptyPreservesTag(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchTag = "main"
	s.BranchInFlight = true
	next, _, _ := d.Step(s, state.DEvJobResult{
		Now:    now,
		Result: BranchDetectResult{Branch: ""},
	})
	gs := next.(GenericState)
	if gs.BranchInFlight {
		t.Error("BranchInFlight should be false")
	}
	if gs.BranchTag != "main" {
		t.Errorf("BranchTag = %q, want main (preserved)", gs.BranchTag)
	}
}

func TestGenericBranchDetectResultErrorPreservesTag(t *testing.T) {
	d, s, now := newGenericState(t, 0)
	s.BranchTag = "main"
	s.BranchInFlight = true
	next, _, _ := d.Step(s, state.DEvJobResult{
		Now:    now,
		Err:    errors.New("git error"),
		Result: BranchDetectResult{Branch: "feature/x"},
	})
	gs := next.(GenericState)
	if gs.BranchInFlight {
		t.Error("BranchInFlight should be false")
	}
	if gs.BranchTag != "main" {
		t.Errorf("BranchTag = %q, want main (preserved on error)", gs.BranchTag)
	}
}

func TestGenericViewIncludesBranchTag(t *testing.T) {
	d, s, _ := newGenericState(t, 0)
	s.BranchTag = "main"
	s.BranchBG = "#89b4fa"
	s.BranchFG = "#1e1e2e"
	v := d.view(s)
	if len(v.Card.Tags) == 0 {
		t.Fatal("expected branch tag in Card.Tags, got none")
	}
	var found bool
	for _, tag := range v.Card.Tags {
		if tag.Text == "main" || strings.Contains(tag.Text, "main") {
			found = true
		}
	}
	if !found {
		t.Errorf("branch tag with text 'main' not found in Tags: %v", v.Card.Tags)
	}
}

func TestParseOscNotif(t *testing.T) {
	tests := []struct {
		name      string
		n         vt.OscNotification
		wantTitle string
		wantBody  string
	}{
		{"osc9 plain", vt.OscNotification{Cmd: 9, Payload: "Hello agent"}, "Hello agent", ""},
		{"osc9 trimmed", vt.OscNotification{Cmd: 9, Payload: "  Hello  "}, "Hello", ""},
		{"osc9 empty", vt.OscNotification{Cmd: 9, Payload: ""}, "", ""},
		{"osc777 title+body", vt.OscNotification{Cmd: 777, Payload: "notify;MyTitle;MyBody"}, "MyTitle", "MyBody"},
		{"osc777 title only", vt.OscNotification{Cmd: 777, Payload: "notify;OnlyTitle"}, "OnlyTitle", ""},
		{"osc777 empty payload", vt.OscNotification{Cmd: 777, Payload: ""}, "", ""},
		{"osc99 body verbatim", vt.OscNotification{Cmd: 99, Payload: "i=1;test message"}, "", "i=1;test message"},
		{"osc99 empty", vt.OscNotification{Cmd: 99, Payload: ""}, "", ""},
		{"unknown cmd", vt.OscNotification{Cmd: 42, Payload: "data"}, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTitle, gotBody := parseOscNotif(tt.n)
			if gotTitle != tt.wantTitle {
				t.Errorf("title = %q, want %q", gotTitle, tt.wantTitle)
			}
			if gotBody != tt.wantBody {
				t.Errorf("body = %q, want %q", gotBody, tt.wantBody)
			}
		})
	}
}
