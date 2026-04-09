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
func (c *Coordinator) Start(ctx context.Context, tickInterval time.Duration) {
	if c.inbox == nil {
		c.inbox = make(chan func(), 32)
		c.stop = make(chan struct{})
		c.stopped = make(chan struct{})
	}
	started := make(chan struct{})
	go c.run(ctx, tickInterval, started)
	<-started
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

// exec submits fn to the actor and blocks until it completes. If the
// actor has already shut down, fn is dropped silently and the caller
// observes the zero value of any captured return variables. exec MUST
// only be called after Start — calling it before Start blocks forever
// because no goroutine is reading from inbox.
func (c *Coordinator) exec(fn func()) {
	done := make(chan struct{})
	select {
	case <-c.stop:
		return
	case c.inbox <- func() { fn(); close(done) }:
	}
	select {
	case <-done:
	case <-c.stopped:
	}
}

// handleTickInternal runs every tick on the actor goroutine. It reaps
// dead sessions, fans the tick out to every Driver, and notifies the
// Server about any state changes via the registered callback.
func (c *Coordinator) handleTickInternal(now time.Time) {
	reaped := c.reapDeadSessionsInternal()
	if len(reaped) > 0 {
		c.fireSessionsChanged()
	}
	if len(c.Sessions.All()) == 0 {
		return
	}
	c.tickInternal(now)
	c.fireSessionsChanged()
}

// fireSessionsChanged builds a sessions-changed Message inside the
// actor and hands it to the registered notifier. The notifier (Server's
// asyncBroadcast) is non-blocking, so the actor never stalls on the
// downstream pipeline.
func (c *Coordinator) fireSessionsChanged() {
	if c.notifySessionsChanged == nil {
		return
	}
	msg := c.buildSessionsEventInternal(false)
	c.notifySessionsChanged(msg)
}

func (c *Coordinator) buildSessionsEventInternal(preview bool) Message {
	msg := NewEvent("sessions-changed")
	msg.Sessions = BuildSessionInfos(c.Sessions.All(), c.Drivers)
	msg.ActiveWindowID = c.activeWindowID
	msg.IsPreview = preview
	return msg
}

// SetSessionsChangedNotifier registers the callback the actor invokes
// after every sessions-changed event. Caller (Server) is responsible for
// making the callback non-blocking. Must be set BEFORE Start.
func (c *Coordinator) SetSessionsChangedNotifier(fn sessionsChangedNotifier) {
	c.notifySessionsChanged = fn
}

