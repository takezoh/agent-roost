package driver

import (
	"log/slog"
	"time"

	"github.com/take/agent-roost/state"
)

// claudeObserver is the per-session state producer for Claude sessions.
// Claude is event-driven: status comes from hook events delivered by the
// `roost claude event` subcommand. Polling is a no-op — if no new event
// arrives after Coordinator startup, the persisted status from
// state.Store.LoadFromTmux is what the user sees.
type claudeObserver struct {
	windowID string
	store    state.Store
}

// NewObserver constructs a Claude observer. Construction does NOT touch
// the store: warm restart relies on the persisted status remaining intact
// until a new hook event explicitly updates it.
func (Claude) NewObserver(windowID string, deps ObserverDeps) Observer {
	return &claudeObserver{windowID: windowID, store: deps.Store}
}

// MarkSpawned writes the initial Idle status, called only when a fresh
// agent process is spawned (Manager.Create / Manager.Recreate). Idle is
// the right default because the agent has just started and hasn't done
// anything yet — Running would imply ongoing work, which the next hook
// event will accurately report.
func (o *claudeObserver) MarkSpawned() {
	if err := o.store.Set(o.windowID, state.Info{
		Status:    state.StatusIdle,
		ChangedAt: time.Now(),
	}); err != nil {
		slog.Warn("claude observer: MarkSpawned set failed", "window", o.windowID, "err", err)
	}
}

// Tick is a no-op for event-driven Claude.
func (o *claudeObserver) Tick(now time.Time) {}

// HandleEvent updates the store from a Claude hook event. Returns true if
// the event was a state-change with a parseable status.
func (o *claudeObserver) HandleEvent(ev AgentEvent) bool {
	if ev.Type != AgentEventStateChange {
		return false
	}
	status, ok := state.ParseStatus(ev.State)
	if !ok {
		return false
	}
	if err := o.store.Set(o.windowID, state.Info{
		Status:    status,
		ChangedAt: time.Now(),
	}); err != nil {
		slog.Warn("claude observer: HandleEvent set failed", "window", o.windowID, "err", err)
		return false
	}
	return true
}

func (o *claudeObserver) Close() {}
