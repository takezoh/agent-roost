package driver

import (
	"regexp"
	"time"

	"github.com/take/agent-roost/state"
)

// Generic driver: polling-driven status producer for non-event-driven
// shells (bash, codex, gemini, fallback). Detects status by hashing
// capture-pane content and watching for prompt indicators / idle
// threshold.
//
// All state lives in GenericState, all I/O is delegated to the worker
// pool via JobCapturePane. Step is a pure function — the same input
// always produces the same output and effects.

const (
	genericPromptPattern = `(?m)(^>|[>$❯]\s*$)`

	// genericKeyStatus / genericKeyStatusChangedAt are the persisted
	// state bag keys for sessions.json round-trip.
	genericKeyStatus          = "status"
	genericKeyStatusChangedAt = "status_changed_at"
)

// genericPromptRegexp is compiled once per process.
var genericPromptRegexp = regexp.MustCompile(genericPromptPattern)

// GenericState is the per-session state for the generic driver. Plain
// data — no goroutines, no I/O.
type GenericState struct {
	state.DriverStateBase

	// Driver name (e.g. "bash", "codex", "gemini", or "" for fallback).
	// Stored on the state so the same generic driver impl can serve
	// multiple registered names.
	Name string

	// Status bookkeeping
	Status          state.Status
	StatusChangedAt time.Time

	// Polling state
	IdleThreshold time.Duration // 0 = idle threshold disabled
	Primed        bool          // true after the first capture-pane baseline
	Hash          string
	LastActivity  time.Time
}

// GenericDriver is the stateless plugin value. Multiple registered
// names share this single value via the Name field on GenericState.
type GenericDriver struct {
	name        string // e.g. "bash"; empty for fallback
	displayName string
	threshold   time.Duration
}

// NewGenericDriver constructs a generic driver registered under the
// given name. Use "" for the fallback driver. The IdleThreshold is
// captured at construction so each driver instance carries its own
// configured value.
func NewGenericDriver(name string, threshold time.Duration) GenericDriver {
	return GenericDriver{
		name:        name,
		displayName: name,
		threshold:   threshold,
	}
}

// WithDisplayName returns a copy with an overridden display name.
func (d GenericDriver) WithDisplayName(name string) GenericDriver {
	d.displayName = name
	return d
}

func (d GenericDriver) Name() string        { return d.name }
func (d GenericDriver) DisplayName() string { return d.displayName }
func (GenericDriver) Status(s state.DriverState) state.Status { return s.(GenericState).Status }

// View returns the cached View for the given GenericState. Pure
// getter — same as the View Step returns, but callable from the
// runtime without going through Step.
func (d GenericDriver) View(s state.DriverState) state.View {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	return d.view(gs)
}

func (d GenericDriver) NewState(now time.Time) state.DriverState {
	return GenericState{
		Name:            d.name,
		Status:          state.StatusIdle,
		StatusChangedAt: now,
		IdleThreshold:   d.threshold,
		LastActivity:    now,
	}
}

// SpawnCommand returns baseCommand unchanged — generic drivers do not
// support resuming a prior agent session.
func (d GenericDriver) SpawnCommand(_ state.DriverState, baseCommand string) string {
	return baseCommand
}

func (d GenericDriver) Persist(s state.DriverState) map[string]string {
	gs, ok := s.(GenericState)
	if !ok {
		return nil
	}
	out := map[string]string{
		genericKeyStatus: gs.Status.String(),
	}
	if !gs.StatusChangedAt.IsZero() {
		out[genericKeyStatusChangedAt] = gs.StatusChangedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (d GenericDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	gs := GenericState{
		Name:            d.name,
		Status:          state.StatusIdle,
		StatusChangedAt: now,
		IdleThreshold:   d.threshold,
		LastActivity:    now,
	}
	if len(bag) == 0 {
		return gs
	}
	if v, ok := bag[genericKeyStatus]; ok && v != "" {
		if status, ok := state.ParseStatus(v); ok {
			changedAt, _ := time.Parse(time.RFC3339, bag[genericKeyStatusChangedAt])
			if changedAt.IsZero() {
				changedAt = now
			}
			gs.Status = status
			gs.StatusChangedAt = changedAt
			gs.LastActivity = changedAt
		}
	}
	return gs
}

// Step is the pure reducer for the generic driver.
func (d GenericDriver) Step(prev state.DriverState, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	gs, ok := prev.(GenericState)
	if !ok {
		gs = d.NewState(time.Time{}).(GenericState)
	}

	switch e := ev.(type) {
	case state.DEvTick:
		// Schedule a capture-pane job for this session's window.
		// Reducer assigns the JobID; we just emit the request.
		if e.WindowID == "" {
			return gs, nil, d.view(gs)
		}
		eff := state.EffStartJob{
			Input: CapturePaneInput{
				WindowID: e.WindowID,
				NLines:   5,
			},
		}
		return gs, []state.Effect{eff}, d.view(gs)

	case state.DEvJobResult:
		if e.Err != nil {
			return gs, nil, d.view(gs)
		}
		result, ok := e.Result.(CapturePaneResult)
		if !ok {
			return gs, nil, d.view(gs)
		}
		return d.applyCapture(gs, e.Now, result), nil, d.view(gs)

	case state.DEvHook:
		// generic drivers don't consume hooks
		return gs, nil, d.view(gs)
	}

	return gs, nil, d.view(gs)
}

// applyCapture is the pure status transition logic. Extracted from
// Step so the test suite can drive it directly without constructing
// DriverEvent values.
func (d GenericDriver) applyCapture(gs GenericState, now time.Time, result CapturePaneResult) GenericState {
	if !gs.Primed {
		gs.Primed = true
		gs.Hash = result.Hash
		if gs.LastActivity.IsZero() {
			gs.LastActivity = now
		}
		return gs
	}

	if result.Hash != gs.Hash {
		next := state.StatusRunning
		if hasPromptIndicator(result.Content, genericPromptRegexp) {
			next = state.StatusWaiting
		}
		gs.Hash = result.Hash
		gs.LastActivity = now
		gs.Status = next
		gs.StatusChangedAt = now
		return gs
	}

	if gs.IdleThreshold > 0 && now.Sub(gs.LastActivity) > gs.IdleThreshold {
		if gs.Status != state.StatusIdle {
			gs.Status = state.StatusIdle
			gs.StatusChangedAt = now
		}
	}
	return gs
}
