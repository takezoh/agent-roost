package driver

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

// promptRe matches common shell prompt endings. Used as a fallback heuristic
// in ShellDriver when no OSC 133 events have been observed for the session.
var promptRe = regexp.MustCompile(`[>$❯%#]\s*$`)

// ShellState is the per-session state for the shell driver. Plain data — no
// goroutines, no I/O.
type ShellState struct {
	CommonState
	PanePolling

	// SawPromptEvent is set on the first OSC 133 event. Once true, promptRe
	// fallback is disabled and only OSC 133 events drive status transitions.
	SawPromptEvent bool

	// LastExitCode is the exit code from the most recent OSC 133;D event.
	// nil means no command has completed yet in this session.
	LastExitCode *int
}

// ShellDriver is the stateless plugin value for "shell"-keyed sessions.
// It extends the polling model with OSC 133 semantic-prompt detection so that
// shells with shell-integration emit accurate status transitions, while shells
// without it fall back to the promptRe heuristic.
type ShellDriver struct {
	name        string
	displayName string
	threshold   time.Duration
}

// NewShellDriver constructs a shell driver registered under the given name.
func NewShellDriver(name, displayName string, threshold time.Duration) ShellDriver {
	return ShellDriver{
		name:        name,
		displayName: displayName,
		threshold:   threshold,
	}
}

func (d ShellDriver) Name() string                          { return d.name }
func (d ShellDriver) DisplayName() string                   { return d.displayName }
func (ShellDriver) Status(s state.DriverState) state.Status { return s.(ShellState).Status }

func (ShellDriver) StartDir(s state.DriverState) string {
	ss, ok := s.(ShellState)
	if !ok {
		return ""
	}
	return ss.StartDir
}

func (ShellDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	ss, ok := s.(ShellState)
	if !ok {
		return s
	}
	ss.StartDir = dir
	return ss
}

func (d ShellDriver) View(s state.DriverState) state.View {
	ss, ok := s.(ShellState)
	if !ok {
		ss = ShellState{}
	}
	return d.view(ss)
}

func (d ShellDriver) NewState(now time.Time) state.DriverState {
	return ShellState{
		CommonState: CommonState{
			Status:          state.StatusWaiting,
			StatusChangedAt: now,
		},
		PanePolling: PanePolling{
			IdleThreshold: d.threshold,
			LastActivity:  now,
		},
	}
}

func (d ShellDriver) PrepareLaunch(s state.DriverState, _ state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	ss, ok := s.(ShellState)
	if !ok {
		ss = ShellState{}
	}
	startDir := project
	req, command := resolveWorktreeRequest(baseCommand, options, "--worktree")
	if ss.StartDir != "" {
		startDir = ss.StartDir
		req.Enabled = true
	}
	return state.LaunchPlan{
		Command:  strings.TrimSpace(command),
		StartDir: startDir,
		Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: req.Enabled}},
		Stdin:    options.InitialInput,
	}, nil
}

func (d ShellDriver) Persist(s state.DriverState) map[string]string {
	ss, ok := s.(ShellState)
	if !ok {
		return nil
	}
	out := make(map[string]string, 12)
	ss.PersistCommon(out)
	if ss.SawPromptEvent {
		out[keyShellSawPromptEvent] = "1"
	}
	if ss.LastExitCode != nil {
		out[keyShellLastExitCode] = fmt.Sprintf("%d", *ss.LastExitCode)
	}
	return out
}

func (d ShellDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	ss := ShellState{
		CommonState: CommonState{
			Status:          state.StatusWaiting,
			StatusChangedAt: now,
		},
		PanePolling: PanePolling{
			IdleThreshold: d.threshold,
			LastActivity:  now,
		},
	}
	if len(bag) == 0 {
		return ss
	}
	ss.RestoreCommon(bag)
	ss.SawPromptEvent = bag[keyShellSawPromptEvent] == "1"
	if v := bag[keyShellLastExitCode]; v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ss.LastExitCode = &n
		}
	}
	return ss
}

const (
	keyShellSawPromptEvent = "shell_saw_prompt_event"
	keyShellLastExitCode   = "shell_last_exit_code"
)

func (d ShellDriver) Step(prev state.DriverState, ctx state.FrameContext, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) { //nolint:funlen
	ss, ok := prev.(ShellState)
	if !ok {
		ss = d.NewState(time.Time{}).(ShellState)
	}

	switch e := ev.(type) {
	case state.DEvTick:
		if !ctx.IsRoot {
			return ss, nil, d.view(ss)
		}
		if !e.Active && ss.Status != state.StatusRunning {
			return ss, nil, d.view(ss)
		}
		effs := paneTickEffects(&ss.CommonState, e)
		return ss, effs, d.view(ss)

	case state.DEvPaneActivity:
		effs := paneActivityEffects(&ss.CommonState, e)
		return ss, effs, d.view(ss)

	case state.DEvJobResult:
		if summary, inFlight, ok := applySummaryJobResult(ss.Summary, ss.SummaryInFlight, e); ok {
			ss.Summary = summary
			ss.SummaryInFlight = inFlight
			return ss, nil, d.view(ss)
		}

		if r, ok := e.Result.(BranchDetectResult); ok {
			ss.BranchInFlight = false
			if e.Err != nil || r.Branch == "" {
				return ss, nil, d.view(ss)
			}
			ss.BranchTag = r.Branch
			ss.BranchBG = r.Background
			ss.BranchFG = r.Foreground
			ss.BranchAt = e.Now
			ss.BranchIsWorktree = r.IsWorktree
			ss.BranchParentBranch = r.ParentBranch
			return ss, nil, d.view(ss)
		}

		result, ok := e.Result.(CapturePaneResult)
		if !ok {
			return ss, nil, d.view(ss)
		}
		if e.Err != nil {
			return ss, nil, d.view(ss)
		}
		next := d.applyCapture(ss, e.Now, result.Snapshot)
		effs, inFlight := d.summaryEffects(ss, next, result)
		next.SummaryInFlight = inFlight
		effs = append(effs, extractOscNotificationEffects(result.Snapshot.Notifications)...)
		return next, effs, d.view(next)

	case state.DEvHook:
		return ss, nil, d.view(ss)
	}

	return ss, nil, d.view(ss)
}

func (d ShellDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string, options state.LaunchOptions) (state.DriverState, state.CreatePlan, error) {
	ss, ok := s.(ShellState)
	if !ok {
		ss = ShellState{}
	}
	plan, name, err := managedWorktreePlan(project, command, options, "--worktree")
	if err != nil {
		return ss, state.CreatePlan{}, err
	}
	if name != "" {
		ss.WorktreeName = name
	}
	return ss, plan, nil
}

func (d ShellDriver) CompleteCreate(s state.DriverState, command string, options state.LaunchOptions, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	ss, ok := s.(ShellState)
	if !ok {
		ss = ShellState{}
	}
	if err != nil {
		return ss, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.StartDir == "" {
		return ss, state.CreateLaunch{}, errors.New("worktree setup did not return a working directory")
	}
	ss.StartDir = r.StartDir
	if r.Name != "" {
		ss.WorktreeName = r.Name
	}
	return ss, state.CreateLaunch{
		Command:  strings.TrimSpace(command),
		StartDir: r.StartDir,
		Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: true}},
	}, nil
}

func (d ShellDriver) ManagedWorktreePath(s state.DriverState) string {
	ss, ok := s.(ShellState)
	if !ok {
		return ""
	}
	return managedWorktreePath(ss.StartDir)
}

// applyCapture applies the OSC 133 → promptRe → idle-threshold cascade.
//
// Priority:
//  1. If PromptEvents is non-empty: use the last event to determine status.
//     Set SawPromptEvent=true to disable regex fallback for this session.
//  2. Else if SawPromptEvent is false: apply promptRe heuristic on stable screen.
//  3. Else fall through to idle-threshold (or stay Running).
func (d ShellDriver) applyCapture(ss ShellState, now time.Time, snap vt.Snapshot) ShellState {
	// OSC 133 priority path — shell-specific, processed before the polling baseline.
	if len(snap.PromptEvents) > 0 {
		ss.SawPromptEvent = true
		last := snap.PromptEvents[len(snap.PromptEvents)-1]
		switch last.Phase {
		case vt.PromptPhaseCommand:
			if ss.Status != state.StatusRunning {
				ss.Status = state.StatusRunning
				ss.StatusChangedAt = now
			}
		case vt.PromptPhaseComplete:
			if last.ExitCode != nil {
				code := *last.ExitCode
				ss.LastExitCode = &code
			}
			if ss.Status != state.StatusWaiting {
				ss.Status = state.StatusWaiting
				ss.StatusChangedAt = now
			}
		}
		// Sync hash here; baseline path is not reached when PromptEvents drive the transition.
		if snap.Stable != ss.Hash {
			ss.Hash = snap.Stable
			ss.LastActivity = now
		}
		return ss
	}

	if applyPollingBaseline(&ss.PanePolling, &ss.CommonState, now, snap) {
		return ss
	}

	// Stable screen — shell-specific promptRe fallback (OSC 133 未観測時のみ).
	if ss.Status == state.StatusRunning && ss.IdleThreshold > 0 {
		if !ss.SawPromptEvent && promptRe.MatchString(snap.LastLine) {
			ss.Status = state.StatusWaiting
			ss.StatusChangedAt = now
			return ss
		}
	}
	applyIdleThresholdFallback(ss.PanePolling, &ss.CommonState, now)
	return ss
}

func (d ShellDriver) summaryEffects(prev, next ShellState, result CapturePaneResult) ([]state.Effect, bool) {
	if next.Status != state.StatusWaiting || prev.Status == state.StatusWaiting {
		return nil, next.SummaryInFlight
	}
	prompt := formatGenericSummaryPrompt(next.Summary, d.displayName, next.StartDir, result.Content)
	return enqueueSummaryJob(nil, next.SummaryInFlight, prompt)
}
