package driver

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

func newShellState(t *testing.T, threshold time.Duration) (ShellDriver, ShellState, time.Time) {
	t.Helper()
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	d := NewShellDriver("shell", "bash", threshold)
	s := d.NewState(now).(ShellState)
	return d, s, now
}

// prime returns a primed ShellState with a baseline hash set.
func primeShell(d ShellDriver, s ShellState, now time.Time) ShellState {
	return d.applyCapture(s, now, vt.Snapshot{Stable: "h0"})
}

// makeRunning transitions the ShellState to Running via a hash change.
func makeShellRunning(d ShellDriver, s ShellState, now time.Time) ShellState {
	s = primeShell(d, s, now)
	t1 := now.Add(time.Millisecond)
	return d.applyCapture(s, t1, vt.Snapshot{Stable: "h1"})
}

func TestShellOsc133CommandEntersRunning(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	// Manually set to Waiting to verify 133;C re-enters Running.
	s.Status = state.StatusWaiting
	t1 := now.Add(2 * time.Second)
	next := d.applyCapture(s, t1, vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseCommand}},
	})
	if next.Status != state.StatusRunning {
		t.Errorf("Status = %v, want Running after OSC 133;C", next.Status)
	}
	if !next.StatusChangedAt.Equal(t1) {
		t.Errorf("StatusChangedAt = %v, want %v", next.StatusChangedAt, t1)
	}
	if !next.SawPromptEvent {
		t.Error("SawPromptEvent should be true after OSC 133")
	}
}

func TestShellOsc133CompleteEntersWaiting(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	if s.Status != state.StatusRunning {
		t.Fatalf("setup: status = %v, want Running", s.Status)
	}
	t1 := now.Add(2 * time.Second)
	code := 0
	next := d.applyCapture(s, t1, vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseComplete, ExitCode: &code}},
	})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting after OSC 133;D", next.Status)
	}
}

func TestShellOsc133CompleteNonZeroExitCode(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	code := 1
	next := d.applyCapture(s, now.Add(time.Second), vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseComplete, ExitCode: &code}},
	})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting", next.Status)
	}
}

func TestShellPromptRegexFallbackNoOsc133(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	if s.Status != state.StatusRunning {
		t.Fatalf("setup: status = %v, want Running", s.Status)
	}
	// No OSC 133 ever seen; promptRe should fire on stable screen.
	t1 := now.Add(time.Second)
	next := d.applyCapture(s, t1, vt.Snapshot{Stable: "h1", LastLine: "user@host:~$ "})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (promptRe fallback)", next.Status)
	}
}

func TestShellPromptRegexDisabledAfterOsc133(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	// Trigger OSC 133 once to set SawPromptEvent.
	s = d.applyCapture(s, now.Add(time.Second), vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseCommand}},
	})
	if !s.SawPromptEvent {
		t.Fatal("SawPromptEvent not set")
	}
	// Stable screen with prompt-looking LastLine — should NOT trigger Waiting
	// because promptRe is disabled after OSC 133 was seen.
	t2 := now.Add(2 * time.Second)
	next := d.applyCapture(s, t2, vt.Snapshot{Stable: "h1", LastLine: "user@host:~$ "})
	if next.Status == state.StatusWaiting {
		t.Error("promptRe should be disabled after OSC 133 was observed")
	}
}

func TestShellIdleThresholdFallback(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	// Stable screen, no prompt, threshold exceeded → Waiting.
	t1 := now.Add(6 * time.Second)
	next := d.applyCapture(s, t1, vt.Snapshot{Stable: "h1", LastLine: "some output"})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting after idle threshold", next.Status)
	}
}

func TestShellPersistRestoreRoundTrip(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s.Status = state.StatusWaiting
	s.StatusChangedAt = now
	s.Summary = "doing stuff"
	s.SawPromptEvent = true

	bag := d.Persist(s)
	if bag[keyShellSawPromptEvent] != "1" {
		t.Errorf("persisted saw_prompt_event = %q, want 1", bag[keyShellSawPromptEvent])
	}

	restored := d.Restore(bag, time.Now()).(ShellState)
	if restored.Status != state.StatusWaiting {
		t.Errorf("restored status = %v, want Waiting", restored.Status)
	}
	if !restored.SawPromptEvent {
		t.Error("restored SawPromptEvent should be true")
	}
	if restored.Summary != "doing stuff" {
		t.Errorf("restored summary = %q, want %q", restored.Summary, "doing stuff")
	}
}

func TestShellOsc133StartPhaseNoStatusChange(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	origStatus := s.Status
	origChangedAt := s.StatusChangedAt
	t1 := now.Add(time.Second)
	next := d.applyCapture(s, t1, vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseStart}},
	})
	if next.Status != origStatus {
		t.Errorf("Status = %v, want unchanged %v on PromptPhaseStart", next.Status, origStatus)
	}
	if !next.StatusChangedAt.Equal(origChangedAt) {
		t.Errorf("StatusChangedAt changed on PromptPhaseStart: got %v, want %v", next.StatusChangedAt, origChangedAt)
	}
	if !next.SawPromptEvent {
		t.Error("SawPromptEvent should be true after OSC 133;A")
	}
}

func TestShellOsc133InputPhaseNoStatusChange(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	origStatus := s.Status
	origChangedAt := s.StatusChangedAt
	t1 := now.Add(time.Second)
	next := d.applyCapture(s, t1, vt.Snapshot{
		Stable:       "h1",
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseInput}},
	})
	if next.Status != origStatus {
		t.Errorf("Status = %v, want unchanged %v on PromptPhaseInput", next.Status, origStatus)
	}
	if !next.StatusChangedAt.Equal(origChangedAt) {
		t.Errorf("StatusChangedAt changed on PromptPhaseInput: got %v, want %v", next.StatusChangedAt, origChangedAt)
	}
	if !next.SawPromptEvent {
		t.Error("SawPromptEvent should be true after OSC 133;B")
	}
}

func TestShellExitCodeSavedOnComplete(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	code := 42
	next := d.applyCapture(s, now, vt.Snapshot{
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseComplete, ExitCode: &code}},
	})
	if next.LastExitCode == nil {
		t.Fatal("LastExitCode should be set after PromptPhaseComplete")
	}
	if *next.LastExitCode != 42 {
		t.Errorf("LastExitCode = %d, want 42", *next.LastExitCode)
	}
}

func TestShellExitCodeZeroSaved(t *testing.T) {
	d, s, now := newShellState(t, 0)
	code := 0
	next := d.applyCapture(s, now, vt.Snapshot{
		PromptEvents: []vt.PromptEvent{{Phase: vt.PromptPhaseComplete, ExitCode: &code}},
	})
	if next.LastExitCode == nil || *next.LastExitCode != 0 {
		t.Errorf("LastExitCode = %v, want 0", next.LastExitCode)
	}
}

func TestShellNoExitCodeNilWhenUnobserved(t *testing.T) {
	d, s, now := newShellState(t, 0)
	next := d.applyCapture(s, now, vt.Snapshot{})
	if next.LastExitCode != nil {
		t.Errorf("LastExitCode should be nil when no PromptPhaseComplete seen, got %d", *next.LastExitCode)
	}
}

func TestShellViewNonZeroExitShowsIndicatorTag(t *testing.T) {
	d, s, now := newShellState(t, 0)
	code := 127
	s.LastExitCode = &code
	_ = now
	v := d.View(s)
	found := false
	for _, tag := range v.Card.Tags {
		if tag.Background == "#cc3333" && tag.Text == "✘ 127" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected exit indicator tag for exit 127, got %v", v.Card.Tags)
	}
}

func TestShellViewZeroExitNoIndicatorTag(t *testing.T) {
	d, s, now := newShellState(t, 0)
	code := 0
	s.LastExitCode = &code
	_ = now
	v := d.View(s)
	for _, tag := range v.Card.Tags {
		if tag.Background == "#cc3333" {
			t.Error("should not show exit indicator tag for exit 0")
		}
	}
}

func TestShellViewNilExitNoIndicatorTag(t *testing.T) {
	d, s, now := newShellState(t, 0)
	_ = now
	v := d.View(s)
	for _, tag := range v.Card.Tags {
		if tag.Background == "#cc3333" {
			t.Error("should not show exit indicator tag when LastExitCode is nil")
		}
	}
}

func TestShellPersistRestoreLastExitCode(t *testing.T) {
	d, s, now := newShellState(t, 0)
	code := 2
	s.LastExitCode = &code
	bag := d.Persist(s)
	restored := d.Restore(bag, now).(ShellState)
	if restored.LastExitCode == nil {
		t.Fatal("LastExitCode not restored from bag")
	}
	if *restored.LastExitCode != 2 {
		t.Errorf("restored LastExitCode = %d, want 2", *restored.LastExitCode)
	}
}

func TestShellOsc133LastEventWins(t *testing.T) {
	// Multiple OSC 133 events in one snapshot: last event takes precedence.
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	code := 0
	t1 := now.Add(time.Second)
	// C then D in same snapshot → result is Waiting (D is last).
	next := d.applyCapture(s, t1, vt.Snapshot{
		Stable: "h1",
		PromptEvents: []vt.PromptEvent{
			{Phase: vt.PromptPhaseCommand},
			{Phase: vt.PromptPhaseComplete, ExitCode: &code},
		},
	})
	if next.Status != state.StatusWaiting {
		t.Errorf("Status = %v, want Waiting (last event wins)", next.Status)
	}
}

// IsRoot=false ガード: 非 root frame は DEvTick / DEvPaneActivity を無視する。

func TestShellStepNonRootSkipsTick(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	next, effs, _ := d.Step(s, state.FrameContext{IsRoot: false}, state.DEvTick{
		Now: now.Add(time.Second), Active: true, Project: "/repo", PaneTarget: "%5",
	})
	if len(effs) != 0 {
		t.Errorf("non-root DEvTick effects = %d, want 0", len(effs))
	}
	if next.(ShellState).Status != s.Status {
		t.Errorf("non-root DEvTick mutated Status: got %v, want %v", next.(ShellState).Status, s.Status)
	}
}

func TestShellStepNonRootSkipsPaneActivity(t *testing.T) {
	d, s, now := newShellState(t, 5*time.Second)
	s = makeShellRunning(d, s, now)
	_, effs, _ := d.Step(s, state.FrameContext{IsRoot: false}, state.DEvPaneActivity{
		PaneTarget: "%5", Now: now.Add(time.Second),
	})
	if len(effs) != 0 {
		t.Errorf("non-root DEvPaneActivity effects = %d, want 0", len(effs))
	}
}
