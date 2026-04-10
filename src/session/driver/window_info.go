package driver

// WindowInfo is the per-session view of the tmux window the Driver is
// attached to. Drivers receive this through Tick(now, win) and never import
// the session package directly. The concrete implementation lives in core
// (windowInfoAdapter) so the Driver knows nothing about session.Session or
// tmux.Client — only this minimal pull interface.
//
// The adapter is constructed by the Coordinator actor goroutine on every
// Tick dispatch and captures a snapshot of the session's static state plus
// whether it is currently active. Drivers therefore observe a consistent
// view that does not change underneath them and never need to call back
// into the Coordinator (which would deadlock against the actor model).
type WindowInfo interface {
	WindowID() string
	AgentPaneID() string
	Project() string
	// Active reports whether this session is currently swapped into the
	// main pane (0.0). Drivers gate expensive periodic work (transcript
	// refresh, branch detection) on this so background sessions stay cheap.
	// The flag is captured at Tick dispatch time and remains constant for
	// the duration of the call.
	Active() bool
	// RecentLines returns the last n lines from the agent pane, or an error
	// if capture failed. Polling drivers (genericDriver) use this; event
	// drivers (claudeDriver) ignore it.
	RecentLines(n int) (string, error)
}
