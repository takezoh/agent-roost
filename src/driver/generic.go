package driver

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

// promptRe matches common shell prompt endings.
// A stable screen whose last non-empty line ends with one of these is
// treated as "at a prompt" → transition to Waiting immediately rather
// than waiting for the full IdleThreshold to expire.
var promptRe = regexp.MustCompile(`[>$❯%#]\s*$`)

// Generic driver: polling-driven status producer for non-event-driven
// shells (bash, codex, gemini, fallback). Detects status by hashing
// capture-pane content; when the hash stays stable for IdleThreshold
// the session transitions Running → Waiting.
//
// All state lives in GenericState, all I/O is delegated to the worker
// pool via JobCapturePane. Step is a pure function — the same input
// always produces the same output and effects.

const genericBranchRefreshInterval = 30 * time.Second

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
func NewGenericDriver(name, displayName string, threshold time.Duration) GenericDriver {
	return GenericDriver{
		name:        name,
		displayName: displayName,
		threshold:   threshold,
	}
}

func (d GenericDriver) Name() string                          { return d.name }
func (d GenericDriver) DisplayName() string                   { return d.displayName }
func (GenericDriver) Status(s state.DriverState) state.Status { return s.(GenericState).Status }

func (GenericDriver) StartDir(s state.DriverState) string {
	gs, ok := s.(GenericState)
	if !ok {
		return ""
	}
	return gs.CommonState.StartDir
}

func (GenericDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	gs, ok := s.(GenericState)
	if !ok {
		return s
	}
	gs.CommonState.StartDir = dir
	return gs
}

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
			// GenericDriver is 2-state (Running/Waiting) — never Idle/Stopped.
			// Start in Waiting so the first capture establishes the baseline
			// before any stability-threshold logic can run.
			Status:          state.StatusWaiting,
			StatusChangedAt: now,
		},
		IdleThreshold: d.threshold,
		LastActivity:  now,
	}
}

func (d GenericDriver) PrepareLaunch(s state.DriverState, _ state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	startDir := project
	req, command := resolveWorktreeRequest(baseCommand, options, "--worktree")
	if gs.StartDir != "" {
		startDir = gs.StartDir
		req.Enabled = true
	}
	return state.LaunchPlan{
		Command:  strings.TrimSpace(command),
		StartDir: startDir,
		Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: req.Enabled}},
		Stdin:    options.InitialInput,
	}, nil
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
			Status:          state.StatusWaiting,
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
		// Tick only when visible on the main pane OR actively running
		// (hash still changing). Parked + waiting sessions skip to save CPU;
		// the next tick after the user brings them back to active resumes.
		if !e.Active && gs.Status != state.StatusRunning {
			return gs, nil, d.view(gs)
		}

		var effs []state.Effect

		// Branch refresh: only when the session is active (swapped into 0.0)
		// and the cache is stale or the working dir changed.
		if e.Active && !gs.BranchInFlight {
			target := gs.StartDir
			if target == "" {
				target = e.Project
			}
			if target != "" && (target != gs.BranchTarget || e.Now.Sub(gs.BranchAt) >= genericBranchRefreshInterval) {
				gs.BranchInFlight = true
				gs.BranchTarget = target
				effs = append(effs, state.EffStartJob{
					Input: BranchDetectInput{WorkingDir: target},
				})
			}
		}

		// Schedule a capture-pane job for this session's pane.
		if e.PaneTarget != "" {
			effs = append(effs, state.EffStartJob{
				Input: CapturePaneInput{
					PaneTarget: e.PaneTarget,
					NLines:     30,
				},
			})
		}

		return gs, effs, d.view(gs)

	case state.DEvJobResult:
		if summary, inFlight, ok := applySummaryJobResult(gs.Summary, gs.SummaryInFlight, e); ok {
			gs.Summary = summary
			gs.SummaryInFlight = inFlight
			return gs, nil, d.view(gs)
		}

		if r, ok := e.Result.(BranchDetectResult); ok {
			gs.BranchInFlight = false
			if e.Err != nil || r.Branch == "" {
				return gs, nil, d.view(gs) // preserve existing tag; retry on next tick
			}
			gs.BranchTag = r.Branch
			gs.BranchBG = r.Background
			gs.BranchFG = r.Foreground
			gs.BranchAt = e.Now
			gs.BranchIsWorktree = r.IsWorktree
			gs.BranchParentBranch = r.ParentBranch
			return gs, nil, d.view(gs)
		}

		result, ok := e.Result.(CapturePaneResult)
		if !ok {
			return gs, nil, d.view(gs)
		}
		if e.Err != nil {
			return gs, nil, d.view(gs)
		}
		next := d.applyCapture(gs, e.Now, result.Snapshot)
		effs, inFlight := d.summaryEffects(gs, next, result)
		next.SummaryInFlight = inFlight
		for _, notif := range result.Snapshot.Notifications {
			title, body := parseOscNotif(notif)
			if title == "" && body == "" {
				continue
			}
			effs = append(effs, state.EffRecordNotification{
				Cmd:   notif.Cmd,
				Title: title,
				Body:  body,
			})
		}
		return next, effs, d.view(next)

	case state.DEvHook:
		// generic drivers don't consume hooks
		return gs, nil, d.view(gs)
	}

	return gs, nil, d.view(gs)
}

func (d GenericDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string, options state.LaunchOptions) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	plan, name, err := managedWorktreePlan(project, command, options, "--worktree")
	if err != nil {
		return gs, state.CreatePlan{}, err
	}
	if name != "" {
		gs.WorktreeName = name
	}
	return gs, plan, nil
}

func (d GenericDriver) CompleteCreate(s state.DriverState, command string, options state.LaunchOptions, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GenericState)
	if !ok {
		gs = GenericState{}
	}
	if err != nil {
		return gs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.StartDir == "" {
		return gs, state.CreateLaunch{}, errors.New("worktree setup did not return a working directory")
	}
	gs.StartDir = r.StartDir
	if r.Name != "" {
		gs.WorktreeName = r.Name
	}
	return gs, state.CreateLaunch{
		Command:  strings.TrimSpace(command),
		StartDir: r.StartDir,
		Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: true}},
	}, nil
}

func (d GenericDriver) ManagedWorktreePath(s state.DriverState) string {
	gs, ok := s.(GenericState)
	if !ok {
		return ""
	}
	return managedWorktreePath(gs.StartDir)
}

// applyCapture is the pure status transition logic. Extracted from
// Step so the test suite can drive it directly without constructing
// DriverEvent values.
//
// VT-snapshot model:
//   - First capture (Primed=false): record baseline Stable/LastActivity.
//   - Screen changed (Stable differs or DirtyCount>0): update baseline; if
//     Waiting, resume Running. Running stays Running without touching
//     StatusChangedAt (preserves elapsed-time display in the UI).
//   - Screen stable: if Running and IdleThreshold>0:
//     • Prompt heuristic: LastLine matches a shell prompt → Waiting immediately.
//     • Idle threshold fallback: elapsed ≥ threshold → Waiting.
//     IdleThreshold=0 disables Waiting entirely.
func (d GenericDriver) applyCapture(gs GenericState, now time.Time, snap vt.Snapshot) GenericState {
	if !gs.Primed {
		gs.Primed = true
		gs.Hash = snap.Stable
		if gs.LastActivity.IsZero() {
			gs.LastActivity = now
		}
		return gs
	}

	if snap.Stable != gs.Hash || snap.DirtyCount > 0 {
		gs.Hash = snap.Stable
		gs.LastActivity = now
		if gs.Status == state.StatusWaiting {
			gs.Status = state.StatusRunning
			gs.StatusChangedAt = now
		}
		// Already Running: do not update StatusChangedAt so elapsed time is preserved.
		return gs
	}

	// Stable screen — check Waiting conditions.
	if gs.Status == state.StatusRunning && gs.IdleThreshold > 0 {
		if promptRe.MatchString(snap.LastLine) {
			gs.Status = state.StatusWaiting
			gs.StatusChangedAt = now
			return gs
		}
		if now.Sub(gs.LastActivity) >= gs.IdleThreshold {
			gs.Status = state.StatusWaiting
			gs.StatusChangedAt = now
		}
	}
	return gs
}

// parseOscNotif extracts title and body from an OSC notification payload.
// OSC 9 (iTerm2): payload is the title text.
// OSC 777 (urxvt): payload is "notify;<title>;<body>".
// OSC 99 (Kitty): payload is key-value; use as body verbatim.
func parseOscNotif(n vt.OscNotification) (title, body string) {
	switch n.Cmd {
	case 9:
		return strings.TrimSpace(n.Payload), ""
	case 777:
		parts := strings.SplitN(n.Payload, ";", 3)
		if len(parts) >= 3 {
			return parts[1], parts[2]
		}
		if len(parts) == 2 {
			return parts[1], ""
		}
	case 99:
		return "", n.Payload
	}
	return "", ""
}

func (d GenericDriver) summaryEffects(prev, next GenericState, result CapturePaneResult) ([]state.Effect, bool) {
	if next.Status != state.StatusWaiting || prev.Status == state.StatusWaiting {
		return nil, next.SummaryInFlight
	}
	prompt := formatGenericSummaryPrompt(next.Summary, d.displayName, next.StartDir, result.Content)
	return enqueueSummaryJob(nil, next.SummaryInFlight, prompt)
}
