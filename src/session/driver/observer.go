package driver

import (
	"time"

	"github.com/take/agent-roost/state"
)

// Observer is the per-session state producer owned by a Driver.
//
// One Observer = one session = one driver. The Observer is the only writer
// to state.Store for its windowID. Implementations may use polling, hook
// reception, or both — that decision lives entirely inside the driver.
//
// Lifecycle:
//   - Driver.NewObserver constructs the Observer. Construction MUST NOT
//     touch the store: warm-restart paths rely on the persisted status
//     surviving observer creation.
//   - MarkSpawned is called only when a fresh agent process is spawned
//     (Manager.Create or Manager.Recreate). It writes the initial Running
//     status and resets internal scratch state.
//   - Tick is called periodically by the Coordinator. Polling drivers use it
//     to capture pane content and detect transitions; event-driven drivers
//     leave it as a no-op.
//   - HandleEvent receives a hook event routed by the Server. Returns true
//     if the observer consumed the event.
//   - Close releases observer-owned resources at session removal.
type Observer interface {
	MarkSpawned()
	Tick(now time.Time)
	HandleEvent(ev AgentEvent) bool
	Close()
}

// PaneCapturer is the minimal capture-pane capability polling-driven
// observers need. Defined here (rather than in tmux) so the driver package
// stays decoupled from tmux and tests can supply a fake.
type PaneCapturer interface {
	CapturePaneLines(paneTarget string, n int) (string, error)
}

// ObserverDeps bundles the runtime dependencies a Driver may need when
// constructing observers. New driver-specific dependencies should be added
// here so the Driver interface stays narrow.
type ObserverDeps struct {
	Store         state.Store
	Capturer      PaneCapturer
	IdleThreshold time.Duration
}
