package core

import (
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

// SessionInfo materialization helpers.
//
// All snapshot reads (AllSessionInfos, SnapshotSessionsAndActive,
// fireSessionsChanged in coordinator_actor.go) follow the same pattern:
// capture (Session, Driver) pairs on the Coordinator actor goroutine,
// then call Driver methods OFF the actor to build SessionInfo records.
// This decouples slow Driver actors from Coordinator inbox processing —
// a Driver mid-Tick delays only its own SessionInfo, never the wider
// snapshot or unrelated commands.

// sessionEntry pairs a Session pointer with its bound Driver. Captured
// on the actor goroutine inside snapshotEntries; the Driver methods on
// the captured value are then invoked from OUTSIDE the actor. Session
// fields used downstream (ID, Project, Command, WindowID, CreatedAt)
// are immutable after spawn so reading them off-actor is race-free.
type sessionEntry struct {
	sess *session.Session
	drv  driver.Driver
}

// snapshotEntries returns the current set of (Session, Driver) pairs
// captured on the actor goroutine. The result is safe to consume from
// any goroutine because the slice is owned by the caller and Sessions
// are immutable after spawn.
func (c *Coordinator) snapshotEntries() []sessionEntry {
	var entries []sessionEntry
	if !c.exec(func() {
		sessions := c.Sessions.All()
		entries = make([]sessionEntry, 0, len(sessions))
		for _, s := range sessions {
			drv, ok := c.Drivers.Get(s.ID)
			if !ok {
				continue
			}
			entries = append(entries, sessionEntry{sess: s, drv: drv})
		}
	}) {
		return nil
	}
	return entries
}

// buildSessionInfosFromEntries materializes SessionInfo records from a
// snapshot of (Session, Driver) entries. Driver method calls (Status,
// View) happen here, OFF the Coordinator actor goroutine — each call
// blocks only on its own Driver actor, never on Coordinator. Slow
// Drivers therefore delay only their own SessionInfo, not the entire
// snapshot, and never starve unrelated Coordinator commands.
func buildSessionInfosFromEntries(entries []sessionEntry) []SessionInfo {
	infos := make([]SessionInfo, 0, len(entries))
	for _, e := range entries {
		info := SessionInfo{
			ID:        e.sess.ID,
			Project:   e.sess.Project,
			Command:   e.sess.Command,
			WindowID:  e.sess.WindowID,
			CreatedAt: e.sess.CreatedAt.Format(time.RFC3339),
		}
		if st, has := e.drv.Status(); has {
			info.State = st.Status
			if !st.ChangedAt.IsZero() {
				info.StateChangedAt = st.ChangedAt.Format(time.RFC3339)
			}
		}
		info.View = e.drv.View()
		infos = append(infos, info)
	}
	return infos
}

// AllSessionInfos returns a snapshot of every session shipped as
// SessionInfo records. The session list is captured on the actor and
// then materialized off-actor so a slow Driver cannot block other
// Coordinator commands.
func (c *Coordinator) AllSessionInfos() []SessionInfo {
	return buildSessionInfosFromEntries(c.snapshotEntries())
}

// SnapshotSessionsAndActive returns sessions + active window. The
// session list and the active id are captured atomically inside one
// actor round-trip; SessionInfo materialization happens off-actor.
func (c *Coordinator) SnapshotSessionsAndActive() (infos []SessionInfo, active string) {
	var entries []sessionEntry
	c.exec(func() {
		sessions := c.Sessions.All()
		entries = make([]sessionEntry, 0, len(sessions))
		for _, s := range sessions {
			drv, ok := c.Drivers.Get(s.ID)
			if !ok {
				continue
			}
			entries = append(entries, sessionEntry{sess: s, drv: drv})
		}
		active = c.activeWindowID
	})
	infos = buildSessionInfosFromEntries(entries)
	return
}
