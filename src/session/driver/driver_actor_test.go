package driver

import (
	"sync"
	"testing"
	"time"
)

// fakeDriver records every method call so the actor wrapper can be
// observed in isolation from any real Driver implementation. Field
// access happens only on the actor goroutine (and the test goroutine
// after Close), so no synchronization is needed beyond the channel
// hand-offs that the tests use.
type fakeDriver struct {
	calls           []string
	tickStartedOnce sync.Once
	tickStarted     chan struct{} // closed exactly once when Tick is first entered
	tickGate        chan struct{} // if non-nil, Tick blocks until closed
	closed          bool
}

func (f *fakeDriver) Name() string                                  { return "fake" }
func (f *fakeDriver) DisplayName() string                           { return "fake" }
func (f *fakeDriver) MarkSpawned()                                  { f.calls = append(f.calls, "MarkSpawned") }
func (f *fakeDriver) HandleEvent(ev AgentEvent) bool                { f.calls = append(f.calls, "HandleEvent"); return true }
func (f *fakeDriver) Status() (StatusInfo, bool)                    { return StatusInfo{Status: StatusIdle}, true }
func (f *fakeDriver) View() SessionView                             { f.calls = append(f.calls, "View"); return SessionView{} }
func (f *fakeDriver) PersistedState() map[string]string             { return map[string]string{"k": "v"} }
func (f *fakeDriver) RestorePersistedState(state map[string]string) {}
func (f *fakeDriver) SpawnCommand(base string) string               { return base }
func (f *fakeDriver) Close()                                        { f.closed = true }

func (f *fakeDriver) Tick(now time.Time, win WindowInfo) {
	if f.tickStarted != nil {
		f.tickStartedOnce.Do(func() { close(f.tickStarted) })
	}
	if f.tickGate != nil {
		<-f.tickGate
	}
	f.calls = append(f.calls, "Tick")
}

func TestDriverActor_SerializesConcurrentCalls(t *testing.T) {
	impl := &fakeDriver{
		tickStarted: make(chan struct{}),
		tickGate:    make(chan struct{}),
	}
	a := newDriverActor(impl)
	t.Cleanup(a.Close)

	// Kick off Tick — it will block on tickGate inside the actor.
	tickDone := make(chan struct{})
	go func() {
		a.Tick(time.Now(), nil)
		close(tickDone)
	}()

	// Wait until Tick is actually executing on the actor goroutine, so
	// the View we queue next is guaranteed to land behind it in the inbox.
	<-impl.tickStarted

	viewDone := make(chan struct{})
	go func() {
		a.View()
		close(viewDone)
	}()

	select {
	case <-viewDone:
		t.Fatal("View ran while Tick was holding the actor")
	case <-time.After(20 * time.Millisecond):
	}

	close(impl.tickGate)
	<-tickDone
	<-viewDone

	if len(impl.calls) != 2 || impl.calls[0] != "Tick" || impl.calls[1] != "View" {
		t.Errorf("call order = %v, want [Tick View]", impl.calls)
	}
}

func TestDriverActor_CloseIsIdempotent(t *testing.T) {
	impl := &fakeDriver{}
	a := newDriverActor(impl)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); a.Close() }()
	go func() { defer wg.Done(); a.Close() }()
	wg.Wait()

	if !impl.closed {
		t.Error("impl.Close was not called")
	}
}

func TestDriverActor_CallsAfterCloseAreNoOps(t *testing.T) {
	impl := &fakeDriver{}
	a := newDriverActor(impl)
	a.Close()

	// Must not panic, must not block forever.
	done := make(chan struct{})
	go func() {
		a.Tick(time.Now(), nil)
		a.View()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("call after Close blocked")
	}
}
