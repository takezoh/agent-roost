package driver

import (
	"log/slog"
	"regexp"
	"sync"
	"time"
)

// Generic driver: polling-driven status producer for non-event-driven shells
// (bash, codex, gemini, fallback). Detects status by hashing capture-pane
// content and watching for prompt indicators / idle threshold.
//
// Two invariants for the warm-restart fix:
//  1. Construction does NOT touch external state. The persisted status
//     restored via RestorePersistedState remains visible until polling
//     observes positive evidence of a transition.
//  2. The first Tick after construction only establishes a baseline hash.
//     status is left untouched. Only the second tick onward writes — and
//     only when a transition is actually observed.

const (
	genericNamePromptPattern = `(?m)(^>|[>$❯]\s*$)`

	genericKeyStatus          = "status"
	genericKeyStatusChangedAt = "status_changed_at"
)

type genericDriver struct {
	mu sync.Mutex

	// Static deps
	name      string
	pattern   *regexp.Regexp
	threshold time.Duration

	// Dynamic state
	status       StatusInfo
	primed       bool
	hash         string
	lastActivity time.Time
}

func newGenericFactory(name string) Factory {
	return func(deps Deps) Driver {
		now := time.Now()
		return &genericDriver{
			name:      name,
			pattern:   regexp.MustCompile(genericNamePromptPattern),
			threshold: deps.IdleThreshold,
			status:    StatusInfo{Status: StatusIdle, ChangedAt: now},
		}
	}
}

func (d *genericDriver) Name() string { return d.name }
func (d *genericDriver) DisplayName() string {
	if d.name == "" {
		return ""
	}
	return d.name
}

// MarkSpawned: a fresh agent process has just started. Reset to Idle and
// drop any cached hash so the first Tick re-establishes baseline.
func (d *genericDriver) MarkSpawned() {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	d.status = StatusInfo{Status: StatusIdle, ChangedAt: now}
	d.primed = false
	d.hash = ""
	d.lastActivity = now
}

// Tick polls the agent pane and updates internal status if a transition is
// observed. The first Tick after construction or restore only establishes
// the baseline hash without touching status — that protects the persisted
// status from being overwritten on warm-restart.
func (d *genericDriver) Tick(now time.Time, win WindowInfo) {
	if win == nil {
		return
	}
	content, err := win.RecentLines(5)
	if err != nil {
		// capture-pane failure does NOT mean the session is dead. Transient
		// reasons (swap-pane race during a fresh spawn, tmux briefly busy,
		// pane index settling) can return an error for a window that is
		// still very much alive. Liveness is the responsibility of
		// SessionService.ReconcileWindows. We just skip this tick.
		slog.Debug("generic driver: capture-pane failed", "window", win.WindowID(), "err", err)
		return
	}
	hash := hashContent(content)

	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.primed {
		// First observation: establish baseline only. Do not touch status —
		// the restored persisted status must survive.
		d.primed = true
		d.hash = hash
		if d.lastActivity.IsZero() {
			d.lastActivity = now
		}
		return
	}

	if hash != d.hash {
		// Pane content changed → real transition.
		next := StatusRunning
		if hasPromptIndicator(content, d.pattern) {
			next = StatusWaiting
		}
		d.hash = hash
		d.lastActivity = now
		d.status = StatusInfo{Status: next, ChangedAt: now}
		return
	}

	// Hash unchanged: idle threshold check.
	if d.threshold > 0 && now.Sub(d.lastActivity) > d.threshold {
		if d.status.Status != StatusIdle {
			d.status = StatusInfo{Status: StatusIdle, ChangedAt: now}
		}
	}
}

// HandleEvent: generic drivers don't consume hook events.
func (d *genericDriver) HandleEvent(ev AgentEvent) bool { return false }

func (d *genericDriver) Close() {}

func (d *genericDriver) Status() (StatusInfo, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.status, true
}

// View returns the minimal SessionView for a generic (non-Claude) session.
// The only driver-specific UI element is the command tag — everything else
// (state symbol, generic INFO header, project name, elapsed time) is
// rendered by the TUI from SessionInfo. Drivers with no display name
// (the unnamed fallback factory) emit no command tag rather than an empty
// colored chip.
func (d *genericDriver) View() SessionView {
	var tags []Tag
	if name := d.DisplayName(); name != "" {
		tags = []Tag{CommandTag(name)}
	}
	return SessionView{
		Card: CardView{Tags: tags},
	}
}

// PersistedState returns the opaque bag for SessionService to round-trip.
// Generic drivers only persist status — they have no agent identity.
func (d *genericDriver) PersistedState() map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make(map[string]string, 2)
	out[genericKeyStatus] = d.status.Status.String()
	if !d.status.ChangedAt.IsZero() {
		out[genericKeyStatusChangedAt] = d.status.ChangedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (d *genericDriver) RestorePersistedState(state map[string]string) {
	if len(state) == 0 {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := state[genericKeyStatus]; ok && s != "" {
		if status, ok := ParseStatus(s); ok {
			changedAt, _ := time.Parse(time.RFC3339, state[genericKeyStatusChangedAt])
			if changedAt.IsZero() {
				changedAt = time.Now()
			}
			d.status = StatusInfo{Status: status, ChangedAt: changedAt}
			d.lastActivity = changedAt
		}
	}
}

// SpawnCommand returns baseCommand unchanged — generic drivers do not
// support resuming a prior agent session.
func (d *genericDriver) SpawnCommand(baseCommand string) string {
	return baseCommand
}
