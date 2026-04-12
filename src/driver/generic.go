package driver

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
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
)

// genericPromptRegexp is compiled once per process.
var genericPromptRegexp = regexp.MustCompile(genericPromptPattern)

// GenericState is the per-session state for the generic driver. Plain
// data — no goroutines, no I/O.
type GenericState struct {
	CommonState

	// Driver name (e.g. "bash", "codex", "gemini", or "" for fallback).
	// Stored on the state so the same generic driver impl can serve
	// multiple registered names.
	Name string

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

func (d GenericDriver) Name() string                          { return d.name }
func (d GenericDriver) DisplayName() string                   { return d.displayName }
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
		Name: d.name,
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
		IdleThreshold: d.threshold,
		LastActivity:  now,
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
	out := make(map[string]string, 10)
	gs.PersistCommon(out)
	return out
}

func (d GenericDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	gs := GenericState{
		Name: d.name,
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
		IdleThreshold: d.threshold,
		LastActivity:  now,
	}
	if len(bag) == 0 {
		return gs
	}
	gs.RestoreCommon(bag)
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
		if e.WindowTarget == "" {
			return gs, nil, d.view(gs)
		}
		eff := state.EffStartJob{
			Input: CapturePaneInput{
				WindowTarget: e.WindowTarget,
				NLines:       5,
			},
		}
		return gs, []state.Effect{eff}, d.view(gs)

	case state.DEvJobResult:
		if summary, inFlight, ok := applySummaryJobResult(gs.Summary, gs.SummaryInFlight, e); ok {
			gs.Summary = summary
			gs.SummaryInFlight = inFlight
			return gs, nil, d.view(gs)
		}

		result, ok := e.Result.(CapturePaneResult)
		if !ok {
			return gs, nil, d.view(gs)
		}
		if e.Err != nil {
			return gs, nil, d.view(gs)
		}
		next := d.applyCapture(gs, e.Now, result)
		effs, inFlight := d.summaryEffects(gs, next, result)
		next.SummaryInFlight = inFlight
		return next, effs, d.view(next)

	case state.DEvHook:
		// generic drivers don't consume hooks
		return gs, nil, d.view(gs)
	}

	return gs, nil, d.view(gs)
}

func (d GenericDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	plan, name, err := managedWorktreePlan(project, command, "--worktree")
	if err != nil {
		return gs, state.CreatePlan{}, err
	}
	if name != "" {
		gs.WorktreeName = name
	}
	return gs, plan, nil
}

func (d GenericDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	if err != nil {
		return gs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.WorkingDir == "" {
		return gs, state.CreateLaunch{}, errors.New("worktree setup did not return a working directory")
	}
	gs.WorkingDir = r.WorkingDir
	if r.Name != "" {
		gs.WorktreeName = r.Name
	}
	_, stripped := parseWorktreeFlags(command, "--worktree")
	return gs, state.CreateLaunch{Command: stripped, StartDir: r.WorkingDir}, nil
}

func (d GenericDriver) ManagedWorktreePath(s state.DriverState) string {
	gs, ok := s.(GenericState)
	if !ok {
		return ""
	}
	return managedWorktreePath(gs.WorkingDir)
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

func (d GenericDriver) summaryEffects(prev, next GenericState, result CapturePaneResult) ([]state.Effect, bool) {
	if next.Status != state.StatusWaiting || prev.Status == state.StatusWaiting {
		return nil, next.SummaryInFlight
	}
	prompt := formatSummaryPrompt(summaryPromptLanguage, next.Summary, []SummaryTurn{
		{Role: "user", Text: strings.TrimSpace(result.Content)},
	})
	return enqueueSummaryJob(nil, next.SummaryInFlight, prompt)
}
