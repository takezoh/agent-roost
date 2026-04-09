package driver

import (
	"errors"
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

type fakeCapturer struct {
	content map[string]string
	err     error
}

func (f *fakeCapturer) CapturePaneLines(target string, n int) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.content[target], nil
}

func newGenericObserver(t *testing.T, content map[string]string, threshold time.Duration) (Observer, state.Store, *fakeCapturer) {
	t.Helper()
	store := state.NewStore(newFakeOptionWriter())
	cap := &fakeCapturer{content: content}
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: threshold,
	})
	return obs, store, cap
}

func TestGenericObserver_ConstructionDoesNotTouchStore(t *testing.T) {
	store := state.NewStore(newFakeOptionWriter())
	store.Set("@1", state.Info{Status: state.StatusWaiting, ChangedAt: time.Now()})

	g := NewGeneric("bash").(Generic)
	_ = g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      &fakeCapturer{content: map[string]string{"@1.0": "x"}},
		IdleThreshold: time.Minute,
	})

	got, _ := store.Get("@1")
	if got.Status != state.StatusWaiting {
		t.Errorf("store after NewObserver = %s, want Waiting (untouched)", got.Status)
	}
}

func TestGenericObserver_FirstTickDoesNotTouchStore(t *testing.T) {
	store := state.NewStore(newFakeOptionWriter())
	store.Set("@1", state.Info{Status: state.StatusPending, ChangedAt: time.Now()})

	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      &fakeCapturer{content: map[string]string{"@1.0": "compiling..."}},
		IdleThreshold: time.Minute,
	})
	obs.Tick(time.Now())

	got, _ := store.Get("@1")
	if got.Status != state.StatusPending {
		t.Errorf("store after first Tick = %s, want Pending (warm-restart preservation)", got.Status)
	}
}

func TestGenericObserver_TransitionDetected(t *testing.T) {
	cap := &fakeCapturer{content: map[string]string{"@1.0": "line1"}}
	store := state.NewStore(newFakeOptionWriter())
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: time.Minute,
	})

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	obs.Tick(now) // baseline only

	cap.content["@1.0"] = "line2"
	obs.Tick(now.Add(time.Second))

	got, ok := store.Get("@1")
	if !ok || got.Status != state.StatusRunning {
		t.Errorf("store after transition = %v ok=%v, want Running true", got, ok)
	}
}

func TestGenericObserver_PromptDetected(t *testing.T) {
	cap := &fakeCapturer{content: map[string]string{"@1.0": "line1"}}
	store := state.NewStore(newFakeOptionWriter())
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: time.Minute,
	})
	now := time.Now()
	obs.Tick(now) // baseline

	cap.content["@1.0"] = "done\n$ "
	obs.Tick(now.Add(time.Second))

	got, _ := store.Get("@1")
	if got.Status != state.StatusWaiting {
		t.Errorf("store after prompt observed = %s, want Waiting", got.Status)
	}
}

func TestGenericObserver_IdleAfterThreshold(t *testing.T) {
	cap := &fakeCapturer{content: map[string]string{"@1.0": "line1"}}
	store := state.NewStore(newFakeOptionWriter())
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: 30 * time.Second,
	})

	now := time.Date(2026, 4, 9, 12, 0, 0, 0, time.UTC)
	obs.Tick(now) // baseline

	cap.content["@1.0"] = "line2"
	obs.Tick(now.Add(time.Second)) // → Running

	// Same content, far past threshold
	obs.Tick(now.Add(2 * time.Minute))

	got, _ := store.Get("@1")
	if got.Status != state.StatusIdle {
		t.Errorf("store after idle threshold = %s, want Idle", got.Status)
	}
}

func TestGenericObserver_UnchangedWithinThreshold(t *testing.T) {
	cap := &fakeCapturer{content: map[string]string{"@1.0": "line1"}}
	store := state.NewStore(newFakeOptionWriter())
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: time.Minute,
	})
	now := time.Now()
	obs.Tick(now) // baseline

	cap.content["@1.0"] = "line2"
	obs.Tick(now.Add(time.Second)) // → Running

	// Same content, within threshold
	obs.Tick(now.Add(5 * time.Second))

	got, _ := store.Get("@1")
	if got.Status != state.StatusRunning {
		t.Errorf("store = %s, want Running (unchanged within threshold)", got.Status)
	}
}

// Regression: capture-pane errors are transient (swap-pane race during a
// fresh spawn, tmux briefly busy, etc.) and must NOT mark a session as
// Stopped. Liveness is the single responsibility of ReconcileWindows;
// genericObserver only reports observed transitions.
func TestGenericObserver_CaptureFailureLeavesStoreUntouched(t *testing.T) {
	store := state.NewStore(newFakeOptionWriter())
	// Pre-populate with the MarkSpawned default so we can verify it survives.
	store.Set("@1", state.Info{Status: state.StatusIdle, ChangedAt: time.Now()})
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      &fakeCapturer{err: errors.New("transient")},
		IdleThreshold: time.Minute,
	})
	obs.Tick(time.Now())

	got, ok := store.Get("@1")
	if !ok || got.Status != state.StatusIdle {
		t.Errorf("store = %v ok=%v, want Idle preserved (capture errors must not write)", got, ok)
	}
}

func TestGenericObserver_HandleEventReturnsFalse(t *testing.T) {
	obs, _, _ := newGenericObserver(t, map[string]string{"@1.0": "x"}, time.Minute)
	if obs.HandleEvent(AgentEvent{Type: AgentEventStateChange, State: "running"}) {
		t.Error("Generic observer should not consume hook events")
	}
}

func TestGenericObserver_MarkSpawnedResetsBaseline(t *testing.T) {
	cap := &fakeCapturer{content: map[string]string{"@1.0": "old"}}
	store := state.NewStore(newFakeOptionWriter())
	g := NewGeneric("bash").(Generic)
	obs := g.NewObserver("@1", ObserverDeps{
		Store:         store,
		Capturer:      cap,
		IdleThreshold: time.Minute,
	})
	obs.MarkSpawned()
	got, _ := store.Get("@1")
	if got.Status != state.StatusIdle {
		t.Errorf("store after MarkSpawned = %s, want Idle", got.Status)
	}

	// First Tick after MarkSpawned should re-establish baseline (no store write)
	obs.Tick(time.Now())
	got, _ = store.Get("@1")
	if got.Status != state.StatusIdle {
		t.Errorf("store after first Tick post-MarkSpawned = %s, want Idle (baseline only)", got.Status)
	}
}
