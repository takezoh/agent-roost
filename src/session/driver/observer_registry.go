package driver

import (
	"log/slog"
	"sync"
	"time"
)

// ObserverRegistry holds the per-session Observer instances and routes
// lifecycle events (Adopt / Spawn / Remove), polling ticks, and hook events
// to the right observer.
//
// Adopt vs Spawn:
//   - Adopt: a session that already exists (warm restart). The store has
//     a persisted status from LoadFromTmux. The observer must not touch
//     the store on construction.
//   - Spawn: a fresh agent process (Manager.Create or Manager.Recreate).
//     MarkSpawned is called so the store reflects the new process.
type ObserverRegistry struct {
	drivers *Registry
	deps    ObserverDeps

	mu        sync.RWMutex
	observers map[string]Observer
}

func NewObserverRegistry(drivers *Registry, deps ObserverDeps) *ObserverRegistry {
	return &ObserverRegistry{
		drivers:   drivers,
		deps:      deps,
		observers: make(map[string]Observer),
	}
}

// Adopt creates an Observer for an existing session without touching the
// store. Used during warm-restart bootstrap after state.Store.LoadFromTmux.
func (r *ObserverRegistry) Adopt(windowID, command string) {
	obs := r.drivers.Get(command).NewObserver(windowID, r.deps)
	r.mu.Lock()
	r.observers[windowID] = obs
	r.mu.Unlock()
}

// Spawn creates an Observer and immediately calls MarkSpawned. Used when
// a fresh agent process has just been started for this window.
func (r *ObserverRegistry) Spawn(windowID, command string) {
	obs := r.drivers.Get(command).NewObserver(windowID, r.deps)
	obs.MarkSpawned()
	r.mu.Lock()
	r.observers[windowID] = obs
	r.mu.Unlock()
}

// Remove drops the observer for windowID and deletes the corresponding
// store entry. Idempotent.
func (r *ObserverRegistry) Remove(windowID string) {
	r.mu.Lock()
	obs := r.observers[windowID]
	delete(r.observers, windowID)
	r.mu.Unlock()
	if obs != nil {
		obs.Close()
	}
	if err := r.deps.Store.Delete(windowID); err != nil {
		slog.Warn("observer registry: store delete failed", "window", windowID, "err", err)
	}
}

// Tick fans out a polling tick to every Observer. Observers run sequentially
// (capture-pane is cheap and the polling cycle already runs on a single
// goroutine). Lock is released before iterating so observers can write to
// the store without lock contention.
func (r *ObserverRegistry) Tick(now time.Time) {
	r.mu.RLock()
	snapshot := make([]Observer, 0, len(r.observers))
	for _, obs := range r.observers {
		snapshot = append(snapshot, obs)
	}
	r.mu.RUnlock()
	for _, obs := range snapshot {
		obs.Tick(now)
	}
}

// Dispatch routes a hook event to the observer responsible for windowID.
// Returns false if no observer is registered for that window.
func (r *ObserverRegistry) Dispatch(windowID string, ev AgentEvent) bool {
	r.mu.RLock()
	obs := r.observers[windowID]
	r.mu.RUnlock()
	if obs == nil {
		return false
	}
	return obs.HandleEvent(ev)
}

// Has reports whether an observer is registered for the given window.
func (r *ObserverRegistry) Has(windowID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.observers[windowID]
	return ok
}
