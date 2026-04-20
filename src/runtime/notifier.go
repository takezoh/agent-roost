package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/takezoh/agent-roost/config"
	libnotify "github.com/takezoh/agent-roost/lib/notify"
	"github.com/takezoh/agent-roost/state"
)

// Notifier dispatches desktop notifications when a state transition
// matches a configured rule. The implementation is kept behind an
// interface so tests can substitute a fake.
type Notifier interface {
	Dispatch(eff state.EffNotify)
	// DispatchOSC fires a toast for an OSC 9/99/777 notification if a matching
	// rule exists. source is "osc9", "osc99", or "osc777".
	DispatchOSC(title, body, source string)
}

// configNotifier is the production Notifier implementation. It matches
// EffNotify against the caller's NotificationsConfig and invokes send
// when a rule matches. send is a field so tests can inject a fake.
type configNotifier struct {
	cfg  *config.NotificationsConfig
	send func(ctx context.Context, title, body string) error
}

// NewNotifier returns a Notifier backed by the given config and the
// provided lib/notify.Notifier. When a rule matches, Dispatch forwards
// the title/body to ln.Send in a goroutine with a 5s timeout.
func NewNotifier(cfg *config.NotificationsConfig, ln *libnotify.Notifier) Notifier {
	return &configNotifier{
		cfg:  cfg,
		send: ln.Send,
	}
}

func (n *configNotifier) Dispatch(eff state.EffNotify) {
	if !n.cfg.AnyMatch(eff.Driver, eff.Command, eff.Project, eff.Kind.String(), "hook") {
		return
	}
	title := notifyTitle(eff)
	body := notifyBody(eff)
	send := n.send
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		send(ctx, title, body) //nolint:errcheck // Send returns nil on unsupported platforms
	}()
}

func notifyTitle(eff state.EffNotify) string {
	return fmt.Sprintf("[%s] %s", eff.Driver, eff.Kind.String())
}

func notifyBody(eff state.EffNotify) string {
	return fmt.Sprintf("%s  %s", eff.Project, eff.Command)
}

func (n *configNotifier) DispatchOSC(title, body, source string) {
	if !n.cfg.AnyMatch("", "", "", "", source) {
		return
	}
	if title == "" && body == "" {
		return
	}
	notifTitle := title
	if notifTitle == "" {
		notifTitle = source
	}
	send := n.send
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		send(ctx, notifTitle, body) //nolint:errcheck
	}()
}

// noopNotifier does nothing. Used when no Notifier is configured.
type noopNotifier struct{}

func (noopNotifier) Dispatch(state.EffNotify)   {}
func (noopNotifier) DispatchOSC(_, _, _ string) {}
