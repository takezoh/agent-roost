package driver

// WindowInfo is the per-session view of the tmux window the Driver is
// attached to. Drivers receive this through Tick(now, win) and never import
// the session package directly. The concrete implementation lives in core
// (windowInfoAdapter) so the Driver knows nothing about session.Session or
// tmux.Client — only this minimal pull interface.
type WindowInfo interface {
	WindowID() string
	AgentPaneID() string
	Project() string
	// RecentLines returns the last n lines from the agent pane, or an error
	// if capture failed. Polling drivers (genericDriver) use this; event
	// drivers (claudeDriver) ignore it.
	RecentLines(n int) (string, error)
}
