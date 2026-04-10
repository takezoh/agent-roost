package core

import (
	"context"
	"log/slog"
	"time"
)

// Coordinator actor primitives.
//
// The Coordinator is implemented as an actor: a single goroutine (started
// by Start) owns SessionService, DriverService, activeWindowID, and the
// sync callbacks. Every state-mutating or state-reading method routes
// through `inbox` so the actor goroutine is the only one that touches
// these fields. The periodic tick + reaper runs from the same goroutine
// via a `time.Ticker` integrated into the select loop.
//
// Cross-actor interaction with the Server actor is one-directional —
// Coordinator notifies Server about session changes via the
// `notifySessionsChanged` callback. The callback uses Server's
// non-blocking inbox path, so the Coordinator actor never has to wait on
// the Server actor (which prevents deadlock cycles).

// notifySessionsChanged is the callback signature Server registers with
// Coordinator. The Message is fully built inside the actor goroutine
// (with current sessions / activeWindowID baked in) and handed to the
// Server's broadcast pipeline.
type sessionsChangedNotifier func(msg Message)

// Start spawns the Coordinator actor goroutine and blocks until it is
// ready to process messages. After Start returns, every public method
// on Coordinator routes through the actor. Init-time methods (Refresh,
// Recreate, SetSyncActive, SetSyncStatus, the optional SetActiveWindowID
// restore) MUST be called BEFORE Start.
//
// Idempotent — second and subsequent calls are no-ops. A double start
// would otherwise spawn two run() goroutines competing for the same
// inbox, breaking actor semantics.
func (c *Coordinator) Start(ctx context.Context, tickInterval time.Duration) {
	c.startOnce.Do(func() {
		c.inbox = make(chan func(), 32)
		c.stop = make(chan struct{})
		c.stopped = make(chan struct{})
		started := make(chan struct{})
		go c.run(ctx, tickInterval, started)
		<-started
	})
}

// Shutdown terminates the Coordinator actor and blocks until the
// goroutine has fully exited. Idempotent — repeated calls are no-ops.
// Distinct from Coordinator.Stop(id) which kills a specific session.
func (c *Coordinator) Shutdown() {
	c.closeOnce.Do(func() {
		if c.stop == nil {
			return
		}
		close(c.stop)
		<-c.stopped
	})
}

// run is the actor goroutine entry point.
func (c *Coordinator) run(ctx context.Context, tickInterval time.Duration, started chan<- struct{}) {
	defer close(c.stopped)
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	slog.Info("coordinator actor started", "tick_interval", tickInterval)
	close(started)
	for {
		select {
		case <-ctx.Done():
			slog.Info("coordinator actor stopping (context done)")
			return
		case <-c.stop:
			slog.Info("coordinator actor stopping (Stop called)")
			return
		case fn := <-c.inbox:
			fn()
		case t := <-ticker.C:
			c.handleTickInternal(t)
		}
	}
}

// exec submits fn to the actor and blocks until it completes. Returns
// true if the closure ran to completion, false if it was dropped because
// the actor was shutting down (or had already exited). Callers that
// need to surface "stopped" as an error wrap the result; void callers
// can ignore it but a Warn-level log is emitted either way so silent
// no-ops are observable in production.
//
// exec MUST only be called after Start — calling it before Start would
// block forever because no goroutine is reading from inbox.
func (c *Coordinator) exec(fn func()) bool {
	done := make(chan struct{})
	select {
	case <-c.stop:
		slog.Warn("coordinator: exec dropped (actor stopped before submission)")
		return false
	case c.inbox <- func() { fn(); close(done) }:
	}
	select {
	case <-done:
		return true
	case <-c.stopped:
		slog.Warn("coordinator: exec dropped (actor stopped while pending)")
		return false
	}
}

// handleTickInternal runs every tick on the actor goroutine. It reaps
// dead sessions and asynchronously fans the tick out to every Driver
// via fanOutTicks; the broadcast for the tick result is fired from
// fanOutTicks itself once every Driver has reported back. The reaper
// notification (if any) is fired here, before the fan-out, so the TUI
// sees an evicted session immediately rather than waiting for the
// fan-out to complete.
func (c *Coordinator) handleTickInternal(now time.Time) {
	reaped := c.reapDeadSessionsInternal()
	if len(reaped) > 0 {
		c.fireSessionsChanged()
	}
	c.dispatchTickFanOut(now)
}

// fireSessionsChanged captures the current (Session, Driver) entries
// and active window id on the actor goroutine, then spawns a worker
// that materializes the SessionInfo payload OUTSIDE the actor and
// invokes the registered notifier. Building infos off-actor is
// essential because BuildSessionInfos calls into Driver actors — doing
// it inline would block the Coordinator actor on whichever Driver is
// slowest, defeating the entire async-tick fan-out design.
//
// Multiple fireSessionsChanged calls in quick succession spawn parallel
// workers; each carries its own snapshot so the notifier may receive
// them out of order. The TUI is idempotent (it just re-renders the
// latest payload) so this is acceptable — and the next tick produces
// a fresh payload that supersedes any straggler.
func (c *Coordinator) fireSessionsChanged() {
	if c.notifySessionsChanged == nil {
		return
	}
	sessions := c.Sessions.All()
	entries := make([]sessionEntry, 0, len(sessions))
	for _, s := range sessions {
		drv, ok := c.Drivers.Get(s.ID)
		if !ok {
			continue
		}
		entries = append(entries, sessionEntry{sess: s, drv: drv})
	}
	activeWid := c.activeWindowID
	notifier := c.notifySessionsChanged
	go func() {
		msg := NewEvent("sessions-changed")
		msg.Sessions = buildSessionInfosFromEntries(entries)
		msg.ActiveWindowID = activeWid
		notifier(msg)
	}()
}

// SetSessionsChangedNotifier registers the callback the actor invokes
// after every sessions-changed event. Caller (Server) is responsible for
// making the callback non-blocking. Must be set BEFORE Start.
func (c *Coordinator) SetSessionsChangedNotifier(fn sessionsChangedNotifier) {
	c.notifySessionsChanged = fn
}

