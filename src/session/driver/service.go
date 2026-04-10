package driver

// DriverService owns the per-session Driver instance map and the Factory
// Registry that builds them. SessionService and DriverService are siblings
// — the only place that knows about both is core.Coordinator, which
// correlates them by sessionID.
//
// DriverService is NOT thread-safe. All methods are called from the
// Coordinator actor goroutine (see core/coordinator.go), which serializes
// access to the drivers map. Each driver value is itself a *driverActor
// that owns its own goroutine — the actor wrapper is the only place
// where per-driver concurrency control matters.
type DriverService struct {
	registry *Registry
	deps     Deps

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
// SessionService.Create has set up the tmux window.
//
// The freshly built impl is wrapped in a driverActor so all subsequent
// calls are serialized through a per-driver goroutine.
func (s *DriverService) Create(sessionID, command string) Driver {
	impl := s.registry.Resolve(command)(s.depsFor(sessionID))
	drv := newDriverActor(impl)
	drv.MarkSpawned()
	s.drivers[sessionID] = drv
	return drv
}

// Restore constructs a new Driver instance for a session that already exists
// (warm or cold restart) and seeds it from a persisted state bag. The bag
// is opaque to DriverService — only the driver knows what its keys mean.
// Restore does NOT call MarkSpawned: status is restored from the bag.
func (s *DriverService) Restore(sessionID, command string, persisted map[string]string) Driver {
	impl := s.registry.Resolve(command)(s.depsFor(sessionID))
	drv := newDriverActor(impl)
	drv.RestorePersistedState(persisted)
	s.drivers[sessionID] = drv
	return drv
}

// depsFor returns a copy of the base Deps with SessionID populated. The
// per-instance bag is what individual driver factories see.
func (s *DriverService) depsFor(sessionID string) Deps {
	deps := s.deps
	deps.SessionID = sessionID
	return deps
}

func (s *DriverService) Get(sessionID string) (Driver, bool) {
	drv, ok := s.drivers[sessionID]
	return drv, ok
}

// Close drops the Driver for sessionID and calls its Close method.
// Idempotent — closing an unknown sessionID is a no-op.
func (s *DriverService) Close(sessionID string) {
	drv := s.drivers[sessionID]
	delete(s.drivers, sessionID)
	if drv != nil {
		drv.Close()
	}
}

// ForEach invokes fn with the current sessionID → Driver mapping.
// Single-threaded by contract — the caller (Coordinator actor) is the
// only goroutine touching the map, so no snapshot is needed.
func (s *DriverService) ForEach(fn func(sessionID string, drv Driver)) {
	for sid, drv := range s.drivers {
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
