package core

import (
	"log/slog"
	"strings"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// Coordinator periodic-tick + reaper logic.
//
// Both run on the Coordinator actor goroutine but the actual Driver
// fan-out is delegated to off-actor worker goroutines (fanOutTicks),
// so a slow Driver never blocks the actor's inbox processing.

// tickJob captures everything fanOutTicks needs to drive one Driver
// without re-entering the Coordinator actor. The driver pointer and
// WindowInfo snapshot are taken on the actor goroutine, so off-actor
// goroutines never read Coordinator state directly.
type tickJob struct {
	sessionID string
	drv       driver.Driver
	win       driver.WindowInfo
}

// dispatchTickFanOut snapshots the current set of (session, driver, win)
// tuples on the actor goroutine and spawns an off-actor worker that
// drives every Driver in parallel via Atomic. The worker re-enters the
// actor through exec() once all results are collected, applies the
// persisted-state writes, and fires sessions-changed.
//
// Concurrent ticks are dropped via tickInFlight: periodic polling is
// idempotent, so a missed tick is corrected by the next one — pile-up
// would just delay state without adding information.
func (c *Coordinator) dispatchTickFanOut(now time.Time) {
	sessions := c.Sessions.All()
	if len(sessions) == 0 {
		return
	}
	jobs := make([]tickJob, 0, len(sessions))
	for _, sess := range sessions {
		drv, ok := c.Drivers.Get(sess.ID)
		if !ok {
			continue
		}
		win := newWindowInfoAdapter(sess, c.Tmux, c.isActiveInternal(sess.WindowID))
		jobs = append(jobs, tickJob{sessionID: sess.ID, drv: drv, win: win})
	}
	if len(jobs) == 0 {
		return
	}
	if !c.tickInFlight.CompareAndSwap(false, true) {
		slog.Debug("tick fan-out skipped: previous tick still in flight")
		return
	}
	go c.fanOutTicks(now, jobs)
}

// fanOutTicks runs OFF the Coordinator actor goroutine. It launches one
// goroutine per Driver, each calling drv.Atomic to run Tick + capture
// PersistedState in a single Driver actor round-trip. After every
// Driver finishes, it re-enters the Coordinator via exec() to apply
// the gathered persisted state and fire one sessions-changed event.
func (c *Coordinator) fanOutTicks(now time.Time, jobs []tickJob) {
	defer c.tickInFlight.Store(false)

	type tickResult struct {
		sessionID string
		persisted map[string]string
	}
	results := make(chan tickResult, len(jobs))

	for _, j := range jobs {
		j := j
		go func() {
			var persisted map[string]string
			j.drv.Atomic(func(d driver.Driver) {
				d.Tick(now, j.win)
				persisted = d.PersistedState()
			})
			results <- tickResult{sessionID: j.sessionID, persisted: persisted}
		}()
	}

	collected := make([]tickResult, 0, len(jobs))
	for i := 0; i < len(jobs); i++ {
		collected = append(collected, <-results)
	}

	if !c.exec(func() {
		for _, r := range collected {
			c.Sessions.UpdatePersistedState(r.sessionID, r.persisted)
		}
		c.fireSessionsChanged()
	}) {
		// Coordinator already shut down — drop the apply silently;
		// nothing observes the result anymore.
		slog.Debug("tick fan-out apply dropped: coordinator stopped")
	}
}

// reapDeadSessionsInternal detects sessions whose tmux window has
// disappeared (agent process exited normally), evicts them from
// SessionService, and closes their Driver instances. Runs on the actor
// goroutine. Returns the removed sessions so handleTickInternal can
// fire a notification.
func (c *Coordinator) reapDeadSessionsInternal() []session.RemovedSession {
	c.reapDeadActivePane00Internal()
	removed, err := c.Sessions.ReconcileWindows()
	if err != nil {
		slog.Error("reconcile windows failed", "err", err)
		return nil
	}
	for _, r := range removed {
		c.Drivers.Close(r.ID)
		c.clearActiveInternal(r.WindowID)
	}
	return removed
}

// reapDeadActivePane00Internal handles the dead-pane case where the
// active session's agent pane has died but the window itself is still
// alive (because the agent pane is currently swapped into pane 0.0 and
// roost:0:0 is remain-on-exit).
func (c *Coordinator) reapDeadActivePane00Internal() {
	out, err := c.Panes.DisplayMessage(c.SessionName+":0.0", "#{pane_dead} #{pane_id}")
	if err != nil {
		return
	}
	parts := strings.Fields(out)
	if len(parts) != 2 || parts[0] != "1" {
		return
	}
	deadPaneID := parts[1]
	owner := c.Sessions.FindByAgentPaneID(deadPaneID)
	if owner == nil {
		return
	}
	slog.Info("reaping dead pane", "pane", deadPaneID, "session", owner.ID, "window", owner.WindowID)
	pane0 := c.SessionName + ":0.0"
	swap := []string{"swap-pane", "-d", "-s", pane0, "-t", owner.WindowID + ".0"}
	if err := c.Panes.RunChain(swap); err != nil {
		slog.Error("reap swap-back failed", "err", err)
		return
	}
	if err := c.Sessions.KillWindow(owner.WindowID); err != nil {
		slog.Error("reap kill-window failed", "err", err)
		return
	}
	c.Drivers.Close(owner.ID)
	c.clearActiveInternal(owner.WindowID)
}
