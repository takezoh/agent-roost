package driver

import (
	"testing"
	"time"

	"github.com/take/agent-roost/state"
)

// TestWarmRestart_ClaudePreservesPersistedStatus is the regression test for
// the original bug: after a Coordinator restart, the persisted status of a
// Claude session must NOT be clobbered if no new hook event arrives.
func TestWarmRestart_ClaudePreservesPersistedStatus(t *testing.T) {
	// Phase 1: prior Coordinator runs, Claude session is in Pending.
	options := newFakeOptionWriter()
	storeA := state.NewStore(options)
	regA := NewObserverRegistry(DefaultRegistry(), ObserverDeps{Store: storeA})
	regA.Spawn("@1", "claude") // → Running
	regA.Dispatch("@1", AgentEvent{Type: AgentEventStateChange, State: "pending"})

	if got, _ := storeA.Get("@1"); got.Status != state.StatusPending {
		t.Fatalf("phase1 storeA = %s, want Pending", got.Status)
	}
	if options.options["@1"][state.OptionStatus] != "pending" {
		t.Fatalf("phase1 tmux %s = %q, want pending",
			state.OptionStatus, options.options["@1"][state.OptionStatus])
	}

	// Phase 2: Coordinator restart. Fresh store + registry, but the same
	// fake tmux holds the persisted user options.
	storeB := state.NewStore(options)
	if err := storeB.LoadFromTmux(&fakeOptionReader{options: options.options}); err != nil {
		t.Fatal(err)
	}
	regB := NewObserverRegistry(DefaultRegistry(), ObserverDeps{Store: storeB})
	regB.Adopt("@1", "claude")

	if got, ok := storeB.Get("@1"); !ok || got.Status != state.StatusPending {
		t.Errorf("after Adopt storeB = %v ok=%v, want Pending true", got, ok)
	}

	// Phase 3: poll loop ticks several times. Claude is event-driven, so
	// every Tick is a no-op and the persisted status is preserved.
	for i := 0; i < 5; i++ {
		regB.Tick(time.Now().Add(time.Duration(i) * time.Second))
	}
	if got, _ := storeB.Get("@1"); got.Status != state.StatusPending {
		t.Errorf("after Tick storeB = %s, want Pending preserved", got.Status)
	}

	// Phase 4: a new hook arrives — the store updates normally.
	regB.Dispatch("@1", AgentEvent{Type: AgentEventStateChange, State: "running"})
	if got, _ := storeB.Get("@1"); got.Status != state.StatusRunning {
		t.Errorf("after new hook = %s, want Running", got.Status)
	}
}

// TestWarmRestart_GenericPreservesPersistedStatus is the same scenario for
// a polling-driven driver (bash). The first Tick after Adopt must be a
// baseline-only no-op.
func TestWarmRestart_GenericPreservesPersistedStatus(t *testing.T) {
	options := newFakeOptionWriter()
	cap := &fakeCapturer{content: map[string]string{"@1.0": "static-pane-content"}}

	storeA := state.NewStore(options)
	regA := NewObserverRegistry(DefaultRegistry(), ObserverDeps{
		Store:         storeA,
		Capturer:      cap,
		IdleThreshold: time.Hour,
	})
	regA.Spawn("@1", "bash") // → Idle (MarkSpawned default)

	// Phase 2: warm restart.
	storeB := state.NewStore(options)
	if err := storeB.LoadFromTmux(&fakeOptionReader{options: options.options}); err != nil {
		t.Fatal(err)
	}
	regB := NewObserverRegistry(DefaultRegistry(), ObserverDeps{
		Store:         storeB,
		Capturer:      cap,
		IdleThreshold: time.Hour,
	})
	regB.Adopt("@1", "bash")

	// First Tick: baseline only. Persisted Idle must remain.
	regB.Tick(time.Now())
	if got, _ := storeB.Get("@1"); got.Status != state.StatusIdle {
		t.Errorf("after first Tick = %s, want Idle preserved", got.Status)
	}

	// Second Tick with same content: still Idle, no transition observed.
	regB.Tick(time.Now().Add(time.Second))
	if got, _ := storeB.Get("@1"); got.Status != state.StatusIdle {
		t.Errorf("after second Tick = %s, want Idle", got.Status)
	}

	// Pane changes → real transition observed → Running written by polling logic.
	cap.content["@1.0"] = "changed-content"
	regB.Tick(time.Now().Add(2 * time.Second))
	if got, _ := storeB.Get("@1"); got.Status != state.StatusRunning {
		t.Errorf("after content-change Tick = %s, want Running", got.Status)
	}
}

// fakeOptionReader implements state.OptionReader against the fake writer's
// in-memory map for round-trip tests.
type fakeOptionReader struct {
	options map[string]map[string]string
}

func (f *fakeOptionReader) ListWindowOptions() (map[string]map[string]string, error) {
	out := make(map[string]map[string]string, len(f.options))
	for k, v := range f.options {
		opts := make(map[string]string, len(v))
		for kk, vv := range v {
			opts[kk] = vv
		}
		out[k] = opts
	}
	return out, nil
}
