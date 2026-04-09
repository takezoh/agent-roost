package driver

import (
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

func newRegistry(t *testing.T) (*ObserverRegistry, state.Store) {
	t.Helper()
	store := state.NewStore(newFakeOptionWriter())
	deps := ObserverDeps{
		Store:         store,
		Capturer:      &fakeCapturer{content: map[string]string{"@1.0": "x", "@2.0": "y"}},
		IdleThreshold: time.Minute,
	}
	drivers := DefaultRegistry()
	return NewObserverRegistry(drivers, deps), store
}

func TestObserverRegistry_AdoptDoesNotMarkSpawned(t *testing.T) {
	reg, store := newRegistry(t)
	store.Set("@1", state.Info{Status: state.StatusPending, ChangedAt: time.Now()})

	reg.Adopt("@1", "claude")

	got, _ := store.Get("@1")
	if got.Status != state.StatusPending {
		t.Errorf("store after Adopt = %s, want Pending (untouched)", got.Status)
	}
}

func TestObserverRegistry_SpawnCallsMarkSpawned(t *testing.T) {
	reg, store := newRegistry(t)

	reg.Spawn("@1", "claude")

	got, ok := store.Get("@1")
	if !ok || got.Status != state.StatusIdle {
		t.Errorf("store after Spawn = %v ok=%v, want Idle true", got, ok)
	}
}

func TestObserverRegistry_RemoveDeletesObserverAndStore(t *testing.T) {
	reg, store := newRegistry(t)
	reg.Spawn("@1", "claude")

	reg.Remove("@1")

	if reg.Has("@1") {
		t.Error("observer still registered after Remove")
	}
	if _, ok := store.Get("@1"); ok {
		t.Error("store entry still present after Remove")
	}
}

func TestObserverRegistry_TickFansOutToAllObservers(t *testing.T) {
	reg, store := newRegistry(t)
	// One claude (no-op Tick) + one bash (polling Tick).
	reg.Spawn("@1", "claude")
	reg.Spawn("@2", "bash")

	now := time.Now()
	// Baseline tick — bash observer establishes hash, claude tick is no-op.
	reg.Tick(now)
	reg.Tick(now.Add(time.Second))

	// claude is still Idle (no event delivered)
	if got, _ := store.Get("@1"); got.Status != state.StatusIdle {
		t.Errorf("@1 (claude) = %s, want Idle", got.Status)
	}
	// bash was Spawned to Idle, baseline established, no transition observed
	if got, _ := store.Get("@2"); got.Status != state.StatusIdle {
		t.Errorf("@2 (bash) = %s, want Idle", got.Status)
	}
}

func TestObserverRegistry_DispatchRoutesToCorrectObserver(t *testing.T) {
	reg, store := newRegistry(t)
	reg.Spawn("@1", "claude")
	reg.Spawn("@2", "claude")

	consumed := reg.Dispatch("@1", AgentEvent{
		Type:  AgentEventStateChange,
		State: "pending",
	})
	if !consumed {
		t.Fatal("Dispatch returned false")
	}

	got1, _ := store.Get("@1")
	got2, _ := store.Get("@2")
	if got1.Status != state.StatusPending {
		t.Errorf("@1 = %s, want Pending", got1.Status)
	}
	if got2.Status != state.StatusIdle {
		t.Errorf("@2 = %s, want Idle (untouched MarkSpawned default)", got2.Status)
	}
}

func TestObserverRegistry_DispatchUnknownWindow(t *testing.T) {
	reg, _ := newRegistry(t)
	if reg.Dispatch("@nope", AgentEvent{Type: AgentEventStateChange, State: "running"}) {
		t.Error("Dispatch should return false for unknown window")
	}
}
