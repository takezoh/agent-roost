package driver

import (
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

type fakeOptionWriter struct {
	options map[string]map[string]string
}

func newFakeOptionWriter() *fakeOptionWriter {
	return &fakeOptionWriter{options: make(map[string]map[string]string)}
}

func (f *fakeOptionWriter) SetWindowUserOptions(windowID string, kv map[string]string) error {
	if _, ok := f.options[windowID]; !ok {
		f.options[windowID] = make(map[string]string)
	}
	for k, v := range kv {
		f.options[windowID][k] = v
	}
	return nil
}

func (f *fakeOptionWriter) UnsetWindowUserOptions(windowID string, keys ...string) error {
	if _, ok := f.options[windowID]; !ok {
		return nil
	}
	for _, k := range keys {
		delete(f.options[windowID], k)
	}
	return nil
}

func newClaudeObserver(t *testing.T) (Observer, state.Store) {
	t.Helper()
	store := state.NewStore(newFakeOptionWriter())
	obs := Claude{}.NewObserver("@1", ObserverDeps{Store: store})
	return obs, store
}

func TestClaudeObserver_ConstructionDoesNotTouchStore(t *testing.T) {
	store := state.NewStore(newFakeOptionWriter())
	// Pre-populate the store as if LoadFromTmux had restored a Pending state.
	store.Set("@1", state.Info{Status: state.StatusPending, ChangedAt: time.Now()})

	_ = Claude{}.NewObserver("@1", ObserverDeps{Store: store})

	got, ok := store.Get("@1")
	if !ok || got.Status != state.StatusPending {
		t.Errorf("store after NewObserver = %v ok=%v, want Pending true", got, ok)
	}
}

func TestClaudeObserver_MarkSpawnedWritesIdle(t *testing.T) {
	obs, store := newClaudeObserver(t)
	obs.MarkSpawned()

	got, ok := store.Get("@1")
	if !ok || got.Status != state.StatusIdle {
		t.Errorf("store after MarkSpawned = %v ok=%v, want Idle true", got, ok)
	}
}

func TestClaudeObserver_TickIsNoop(t *testing.T) {
	store := state.NewStore(newFakeOptionWriter())
	store.Set("@1", state.Info{Status: state.StatusPending, ChangedAt: time.Now()})
	obs := Claude{}.NewObserver("@1", ObserverDeps{Store: store})

	for i := 0; i < 5; i++ {
		obs.Tick(time.Now())
	}

	got, _ := store.Get("@1")
	if got.Status != state.StatusPending {
		t.Errorf("store after Tick = %s, want Pending (Tick must be no-op)", got.Status)
	}
}

func TestClaudeObserver_HandleEventStateChange(t *testing.T) {
	obs, store := newClaudeObserver(t)
	obs.MarkSpawned()

	consumed := obs.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "pending",
	})
	if !consumed {
		t.Fatal("HandleEvent returned false for valid state-change")
	}
	got, _ := store.Get("@1")
	if got.Status != state.StatusPending {
		t.Errorf("store = %s, want Pending", got.Status)
	}
}

func TestClaudeObserver_HandleEventUnknownStateRejected(t *testing.T) {
	obs, store := newClaudeObserver(t)
	obs.MarkSpawned()

	consumed := obs.HandleEvent(AgentEvent{
		Type:  AgentEventStateChange,
		State: "garbage",
	})
	if consumed {
		t.Fatal("HandleEvent should reject unknown status")
	}
	got, _ := store.Get("@1")
	if got.Status != state.StatusIdle {
		t.Errorf("store = %s, want unchanged Idle (the MarkSpawned default)", got.Status)
	}
}

func TestClaudeObserver_HandleEventNonStateChangeIgnored(t *testing.T) {
	obs, store := newClaudeObserver(t)
	obs.MarkSpawned()
	store.Set("@1", state.Info{Status: state.StatusWaiting, ChangedAt: time.Now()})

	consumed := obs.HandleEvent(AgentEvent{
		Type: AgentEventSessionStart,
	})
	if consumed {
		t.Error("session-start should not be consumed by claudeObserver state path")
	}
	got, _ := store.Get("@1")
	if got.Status != state.StatusWaiting {
		t.Errorf("store = %s, want unchanged Waiting", got.Status)
	}
}
