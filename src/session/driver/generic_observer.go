package driver

import (
	"log/slog"
	"regexp"
	"time"

	"github.com/take/agent-roost/state"
)

// genericObserver is the per-session state producer for non-event-driven
// drivers (codex, gemini, bash, fallback). It detects status by polling
// capture-pane content with a hash + idle threshold heuristic.
//
// Two invariants matter for the warm-restart fix:
//  1. Construction does NOT touch the store. The persisted status from
//     state.Store.LoadFromTmux remains visible until the polling loop
//     observes positive evidence of a transition.
//  2. The first Tick after construction only establishes a baseline hash.
//     The store is left untouched. Only the SECOND tick onward writes —
//     and only when a transition is actually observed.
type genericObserver struct {
	windowID  string
	store     state.Store
	capturer  PaneCapturer
	pattern   *regexp.Regexp
	threshold time.Duration

	primed       bool
	hash         string
	lastActivity time.Time
}

// NewObserver constructs a Generic observer. Construction reads the prior
// ChangedAt from the store (if any) so the idle countdown remains continuous
// across Coordinator restarts. The store itself is not modified.
func (g Generic) NewObserver(windowID string, deps ObserverDeps) Observer {
	o := &genericObserver{
		windowID:  windowID,
		store:     deps.Store,
		capturer:  deps.Capturer,
		pattern:   regexp.MustCompile(genericPromptPattern),
		threshold: deps.IdleThreshold,
	}
	if info, ok := deps.Store.Get(windowID); ok {
		o.lastActivity = info.ChangedAt
	}
	return o
}

// MarkSpawned writes the initial Idle status, called only when a fresh
// agent process is spawned (Manager.Create / Manager.Recreate). Idle is
// the right default because the agent has just started and hasn't done
// anything yet — Running implies ongoing work, which the next polling
// transition will accurately report when the pane content changes.
func (o *genericObserver) MarkSpawned() {
	now := time.Now()
	if err := o.store.Set(o.windowID, state.Info{
		Status:    state.StatusIdle,
		ChangedAt: now,
	}); err != nil {
		slog.Warn("generic observer: MarkSpawned set failed", "window", o.windowID, "err", err)
	}
	o.primed = false
	o.hash = ""
	o.lastActivity = now
}

func (o *genericObserver) Tick(now time.Time) {
	if o.capturer == nil {
		return
	}
	content, err := o.capturer.CapturePaneLines(o.windowID+".0", 5)
	if err != nil {
		// capture-pane failure does NOT mean the session is dead. Transient
		// reasons (swap-pane race during a fresh spawn, tmux briefly busy,
		// pane index settling) can return an error for a window that is
		// still very much alive. Liveness is the single responsibility of
		// Manager.ReconcileWindows (called from Service.ReapDeadSessions
		// each polling tick), which evicts the session entirely if its
		// window is gone. We just skip this tick and try again next time.
		slog.Debug("generic observer: capture-pane failed", "window", o.windowID, "err", err)
		return
	}
	hash := hashContent(content)

	if !o.primed {
		// First observation: establish baseline only. Do not touch the store —
		// the persisted status from LoadFromTmux must survive observer creation.
		o.primed = true
		o.hash = hash
		if o.lastActivity.IsZero() {
			o.lastActivity = now
		}
		return
	}

	if hash != o.hash {
		// Pane content changed → real transition.
		status := state.StatusRunning
		if hasPromptIndicator(content, o.pattern) {
			status = state.StatusWaiting
		}
		o.hash = hash
		o.lastActivity = now
		if err := o.store.Set(o.windowID, state.Info{Status: status, ChangedAt: now}); err != nil {
			slog.Warn("generic observer: transition set failed", "window", o.windowID, "err", err)
		}
		return
	}

	// Hash unchanged: idle threshold check.
	if o.threshold > 0 && now.Sub(o.lastActivity) > o.threshold {
		cur, ok := o.store.Get(o.windowID)
		if !ok || cur.Status != state.StatusIdle {
			if err := o.store.Set(o.windowID, state.Info{Status: state.StatusIdle, ChangedAt: now}); err != nil {
				slog.Warn("generic observer: idle set failed", "window", o.windowID, "err", err)
			}
		}
	}
}

// HandleEvent returns false: generic drivers do not consume hook events.
func (o *genericObserver) HandleEvent(ev AgentEvent) bool { return false }

func (o *genericObserver) Close() {}
