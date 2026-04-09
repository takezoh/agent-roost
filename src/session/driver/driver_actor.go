package driver

import (
	"sync"
	"time"
)

// driverActor wraps a Driver impl in a single goroutine + inbox channel so
// every method call is serialized through the actor's run loop. The wrapped
// impl is touched only by the actor goroutine, which removes the need for
// any mutex inside the impl itself.
//
// Public methods implement the Driver interface and dispatch each call as a
// closure that runs on the actor goroutine. Each call blocks until the
// closure finishes, so the public surface looks identical to a plain
// synchronous method call.
//
// Name / DisplayName bypass the actor: both are set at construction and
// never mutated, so reading them concurrently is safe and avoids paying for
// an inbox round trip on the hot path (BuildSessionInfos calls them once
// per driver per broadcast).
type driverActor struct {
	impl      Driver
	inbox     chan func()
	stop      chan struct{} // closed by Close to ask run to exit
	stopped   chan struct{} // closed by run when it actually exits
	closeOnce sync.Once
}

// newDriverActor wraps impl in an actor and starts its run loop. The
// returned *driverActor takes ownership of impl — callers must not access
// impl directly after this point.
func newDriverActor(impl Driver) *driverActor {
	a := &driverActor{
		impl:    impl,
		inbox:   make(chan func(), 16),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go a.run()
	return a
}

// run executes queued closures one at a time on the actor goroutine.
// On stop, it drains any closures that were already enqueued so submit()
// callers waiting on done channels are not left hanging.
func (a *driverActor) run() {
	defer close(a.stopped)
	for {
		select {
		case fn := <-a.inbox:
			fn()
		case <-a.stop:
			for {
				select {
				case fn := <-a.inbox:
					fn()
				default:
					return
				}
			}
		}
	}
}

// submit enqueues fn and blocks until the actor goroutine finishes running
// it. If the actor has already been shut down, fn is silently dropped — the
// caller observes the zero value of whatever it captured.
func (a *driverActor) submit(fn func()) {
	done := make(chan struct{})
	wrap := func() {
		fn()
		close(done)
	}
	select {
	case <-a.stop:
		return
	case a.inbox <- wrap:
	}
	select {
	case <-done:
	case <-a.stopped:
	}
}

func (a *driverActor) Name() string        { return a.impl.Name() }
func (a *driverActor) DisplayName() string { return a.impl.DisplayName() }

func (a *driverActor) MarkSpawned() {
	a.submit(func() { a.impl.MarkSpawned() })
}

func (a *driverActor) Tick(now time.Time, win WindowInfo) {
	a.submit(func() { a.impl.Tick(now, win) })
}

func (a *driverActor) HandleEvent(ev AgentEvent) bool {
	var consumed bool
	a.submit(func() { consumed = a.impl.HandleEvent(ev) })
	return consumed
}

func (a *driverActor) Status() (StatusInfo, bool) {
	var info StatusInfo
	var ok bool
	a.submit(func() { info, ok = a.impl.Status() })
	return info, ok
}

func (a *driverActor) View() SessionView {
	var v SessionView
	a.submit(func() { v = a.impl.View() })
	return v
}

func (a *driverActor) PersistedState() map[string]string {
	var s map[string]string
	a.submit(func() { s = a.impl.PersistedState() })
	return s
}

func (a *driverActor) RestorePersistedState(state map[string]string) {
	a.submit(func() { a.impl.RestorePersistedState(state) })
}

func (a *driverActor) SpawnCommand(baseCommand string) string {
	var cmd string
	a.submit(func() { cmd = a.impl.SpawnCommand(baseCommand) })
	return cmd
}

// Close runs the impl's Close on the actor goroutine, then shuts down the
// actor itself. Idempotent — repeated calls are no-ops thanks to closeOnce.
func (a *driverActor) Close() {
	a.closeOnce.Do(func() {
		a.submit(func() { a.impl.Close() })
		close(a.stop)
		<-a.stopped
	})
}

// unwrapDriver returns the underlying impl when d is a *driverActor, or d
// itself otherwise. Test-only convenience for code that needs to inspect
// driver internals (the production hot path always goes through the actor
// surface).
func unwrapDriver(d Driver) Driver {
	if a, ok := d.(*driverActor); ok {
		return a.impl
	}
	return d
}
