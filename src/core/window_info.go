package core

import (
	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
	"github.com/take/agent-roost/tmux"
)

// windowInfoAdapter binds a *session.Session to a *tmux.Client to satisfy
// the driver.WindowInfo interface. It is the only place outside of
// SessionService that touches both Session and tmux at once. Drivers
// receive this through Coordinator.Tick — they never see Session or
// tmux.Client directly.
type windowInfoAdapter struct {
	sess *session.Session
	tmux *tmux.Client
}

func newWindowInfoAdapter(sess *session.Session, t *tmux.Client) driver.WindowInfo {
	return &windowInfoAdapter{sess: sess, tmux: t}
}

func (a *windowInfoAdapter) WindowID() string    { return a.sess.WindowID }
func (a *windowInfoAdapter) AgentPaneID() string { return a.sess.AgentPaneID }
func (a *windowInfoAdapter) Project() string     { return a.sess.Project }

func (a *windowInfoAdapter) RecentLines(n int) (string, error) {
	target := a.sess.WindowID + ".0"
	return a.tmux.CapturePaneLines(target, n)
}
