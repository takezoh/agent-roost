package driver

import (
	"sync"
)

// DriverService owns the per-session Driver instance map and the Factory
// Registry that builds them. SessionService and DriverService are siblings
// — the only place that knows about both is core.Coordinator, which
// correlates them by sessionID.
type DriverService struct {
	registry *Registry
	deps     Deps

	mu      sync.RWMutex
	drivers map[string]Driver // sessionID → Driver
}

func NewDriverService(registry *Registry, deps Deps) *DriverService {
	return &DriverService{
		registry: registry,
		deps:     deps,
		drivers:  make(map[string]Driver),
	}
}

// Create constructs a new Driver instance for a fresh session and
// immediately calls MarkSpawned. Used by Coordinator.Create after
// SessionService.Create has set up the tmux window. The sessionCtx is
// merged into the per-instance Deps so Drivers can pull active state.
func (s *DriverService) Create(sessionID, command string, sessionCtx SessionContext) Driver {
	drv := s.registry.Resolve(command)(s.depsFor(sessionCtx))
	drv.MarkSpawned()
	s.mu.Lock()
	s.drivers[sessionID] = drv
	s.mu.Unlock()
	return drv
}

// Restore constructs a new Driver instance for a session that already exists
// (warm or cold restart) and seeds it from a persisted state bag. The bag
// is opaque to DriverService — only the driver knows what its keys mean.
// Restore does NOT call MarkSpawned: status is restored from the bag.
func (s *DriverService) Restore(sessionID, command string, persisted map[string]string, sessionCtx SessionContext) Driver {
	drv := s.registry.Resolve(command)(s.depsFor(sessionCtx))
	drv.RestorePersistedState(persisted)
	s.mu.Lock()
	s.drivers[sessionID] = drv
	s.mu.Unlock()
	return drv
}

// depsFor returns a copy of the base Deps with Session populated. A nil
// sessionCtx falls back to inactiveSessionContext{} so Drivers always have
// a non-nil interface to call.
func (s *DriverService) depsFor(sessionCtx SessionContext) Deps {
	deps := s.deps
	if sessionCtx == nil {
		deps.Session = inactiveSessionContext{}
	} else {
		deps.Session = sessionCtx
	}
	return deps
}

func (s *DriverService) Get(sessionID string) (Driver, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	drv, ok := s.drivers[sessionID]
	return drv, ok
}

// Close drops the Driver for sessionID and calls its Close method.
// Idempotent — closing an unknown sessionID is a no-op.
func (s *DriverService) Close(sessionID string) {
	s.mu.Lock()
	drv := s.drivers[sessionID]
	delete(s.drivers, sessionID)
	s.mu.Unlock()
	if drv != nil {
		drv.Close()
	}
}

// ForEach invokes fn with a stable snapshot of the current sessionID →
// Driver mapping. The lock is released before fn runs so callbacks can call
// other DriverService methods without deadlock.
func (s *DriverService) ForEach(fn func(sessionID string, drv Driver)) {
	s.mu.RLock()
	snapshot := make(map[string]Driver, len(s.drivers))
	for k, v := range s.drivers {
		snapshot[k] = v
	}
	s.mu.RUnlock()
	for sid, drv := range snapshot {
		fn(sid, drv)
	}
}

// DisplayName returns the user-facing label for the command's driver.
// Convenience proxy to Registry.DisplayName so callers don't need to grab
// the registry separately.
func (s *DriverService) DisplayName(command string) string {
	return s.registry.DisplayName(command)
}

// Registry exposes the underlying factory registry. Used by tests and the
// TUI palette which needs to enumerate command kinds.
func (s *DriverService) Registry() *Registry {
	return s.registry
}
