package runtime

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestStoreAndInvokeFrameCleanup(t *testing.T) {
	r := New(Config{})

	var called atomic.Bool
	r.storeFrameCleanup("f1", func() error {
		called.Store(true)
		return nil
	})

	r.invokeFrameCleanup("f1")

	// invokeFrameCleanup runs the fn in a goroutine; wait briefly.
	deadline := time.Now().Add(200 * time.Millisecond)
	for !called.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !called.Load() {
		t.Error("cleanup fn was not called after invokeFrameCleanup")
	}

	// Second invoke for same frame should be a no-op (already deleted).
	called.Store(false)
	r.invokeFrameCleanup("f1")
	time.Sleep(20 * time.Millisecond)
	if called.Load() {
		t.Error("cleanup fn called twice for same frame")
	}
}

func TestInvokeFrameCleanup_noopWhenNil(t *testing.T) {
	r := New(Config{})
	// No cleanup registered; must not panic.
	r.invokeFrameCleanup("unknown")
}

func TestDrainFrameCleanups(t *testing.T) {
	r := New(Config{})

	var count atomic.Int32
	for _, id := range []state.FrameID{"f1", "f2", "f3"} {
		r.storeFrameCleanup(id, func() error {
			count.Add(1)
			return nil
		})
	}

	r.drainFrameCleanups()

	if got := count.Load(); got != 3 {
		t.Errorf("drain called %d cleanups, want 3", got)
	}

	// Map must be empty after drain.
	r.frameCleanupsMu.Lock()
	remaining := len(r.frameCleanups)
	r.frameCleanupsMu.Unlock()
	if remaining != 0 {
		t.Errorf("frameCleanups has %d entries after drain, want 0", remaining)
	}
}

func TestInvokeFrameCleanup_errorLogged(t *testing.T) {
	r := New(Config{})
	r.storeFrameCleanup("ferr", func() error {
		return errors.New("container stop failed")
	})
	// Must not panic; the error is logged internally.
	r.invokeFrameCleanup("ferr")
	time.Sleep(20 * time.Millisecond)
}

func TestDirectLauncher_adoptFrame_noop(t *testing.T) {
	l := DirectLauncher{}
	cleanup, err := l.AdoptFrame(context.Background(), state.FrameID("f1"), "/workspace/foo", "")
	if err != nil {
		t.Fatalf("AdoptFrame returned error: %v", err)
	}
	if cleanup != nil {
		t.Error("DirectLauncher.AdoptFrame should return nil cleanup")
	}
}

// TestRunShutdown_doesNotDrainCleanups verifies that cancelling the runtime
// context does not invoke frame cleanup callbacks — containers must survive
// daemon shutdown so tmux panes stay alive for warm-restart adoption.
func TestRunShutdown_doesNotDrainCleanups(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var called atomic.Bool
	r := New(Config{Tmux: newFakeTmux()})
	r.storeFrameCleanup("f-shutdown", func() error {
		called.Store(true)
		return nil
	})

	go func() { _ = r.Run(ctx) }()
	cancel()
	select {
	case <-r.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("runtime did not stop within timeout")
	}

	// Allow a brief window for any async goroutines to run.
	time.Sleep(50 * time.Millisecond)
	if called.Load() {
		t.Error("frame cleanup must NOT be called on daemon shutdown (warm-restart requires containers to survive)")
	}
}

func TestEffKillSessionWindow_invokesCleanup(t *testing.T) {
	var called atomic.Bool
	tmux := &fakeTmux{}
	r := New(Config{Tmux: tmux})

	frameID := state.FrameID("f-kill")
	r.sessionPanes[frameID] = "%42"
	r.storeFrameCleanup(frameID, func() error {
		called.Store(true)
		return nil
	})

	r.execute(state.EffKillSessionWindow{FrameID: frameID})

	deadline := time.Now().Add(200 * time.Millisecond)
	for !called.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !called.Load() {
		t.Error("cleanup not called after EffKillSessionWindow")
	}
}
