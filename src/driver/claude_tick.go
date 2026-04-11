package driver

import "github.com/takezoh/agent-roost/state"

// Tick / fsnotify / job-result handling for the Claude driver.

// handleTick is fired by the global ticker. The Claude driver is
// event-driven so periodic work is limited to:
//   - branch tag refresh (active sessions only)
//   - pane capture for hang detection (background Running sessions only)
func (d ClaudeDriver) handleTick(cs ClaudeState, e state.DEvTick) (ClaudeState, []state.Effect) {
	var effs []state.Effect

	// Branch refresh: only when the session is active (swapped into 0.0)
	// and the cache is stale or the working dir changed. The job is async;
	// the in-flight flag prevents pile-up.
	if e.Active {
		target := cs.WorkingDir
		if target == "" {
			target = e.Project
		}
		if target != "" && !cs.BranchInFlight {
			if target != cs.BranchTarget || e.Now.Sub(cs.BranchAt) >= claudeBranchRefreshInterval {
				cs.BranchInFlight = true
				cs.BranchTarget = target
				effs = append(effs, state.EffStartJob{
					Input: BranchDetectInput{WorkingDir: target},
				})
			}
		}
	}

	// Pane capture for hang detection: background Running sessions only.
	// When active, the agent pane is swapped into 0.0 and WindowID.0
	// holds the main TUI — capturing it would be meaningless. The user
	// can see the active session directly; hang detection adds no value.
	if !e.Active && cs.Status == state.StatusRunning && e.WindowID != "" && !cs.CaptureInFlight {
		cs.CaptureInFlight = true
		effs = append(effs, state.EffStartJob{
			Input: CapturePaneInput{WindowID: e.WindowID, NLines: 5},
		})
	}

	// Hang threshold check: if Running, pane is primed, no subagents
	// are active, and neither pane content nor hook events have changed
	// for claudeHangThreshold, transition to Idle.
	if cs.Status == state.StatusRunning && cs.PaneHash != "" && !hasActiveSubagents(cs) {
		lastActivity := cs.PaneHashAt
		if cs.StatusChangedAt.After(lastActivity) {
			lastActivity = cs.StatusChangedAt
		}
		if e.Now.Sub(lastActivity) > claudeHangThreshold {
			cs.Status = state.StatusIdle
			cs.StatusChangedAt = e.Now
			cs.HangDetected = true
			effs = append(effs, state.EffEventLogAppend{
				Line: "HangDetected (pane unchanged)",
			})
		}
	}

	return cs, effs
}

func hasActiveSubagents(cs ClaudeState) bool {
	for _, n := range cs.SubagentCounts {
		if n > 0 {
			return true
		}
	}
	return false
}

// handleTranscriptChanged fires when fsnotify reports a write to the
// active session's transcript file. We schedule an incremental parse
// (deduped via TranscriptInFlight).
func (d ClaudeDriver) handleTranscriptChanged(cs ClaudeState, e state.DEvFileChanged) (ClaudeState, []state.Effect) {
	if cs.TranscriptInFlight {
		return cs, nil
	}
	path := cs.TranscriptPath
	if path == "" {
		path = e.Path
	}
	if path == "" {
		return cs, nil
	}
	cs.TranscriptInFlight = true
	return cs, []state.Effect{
		state.EffStartJob{
			Input: TranscriptParseInput{
				ClaudeUUID: cs.ClaudeSessionID,
				Path:       path,
			},
		},
	}
}

// handleJobResult routes a finished worker pool result back to the
// matching field on ClaudeState.
func (d ClaudeDriver) handleJobResult(cs ClaudeState, e state.DEvJobResult) (ClaudeState, []state.Effect) {
	switch r := e.Result.(type) {
	case TranscriptParseResult:
		cs.TranscriptInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		if r.Title != "" {
			cs.Title = r.Title
		}
		if r.LastPrompt != "" {
			cs.LastPrompt = r.LastPrompt
		}
		cs.StatusLine = r.StatusLine
		cs.CurrentTool = r.CurrentTool
		cs.SubagentCounts = r.Subagents
		return cs, nil

	case HaikuSummaryResult:
		cs.SummaryInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		if r.Summary != "" {
			cs.Summary = r.Summary
		}
		return cs, nil

	case BranchDetectResult:
		cs.BranchInFlight = false
		if e.Err != nil || r.Branch == "" {
			return cs, nil // preserve existing tag; retry on next tick
		}
		cs.BranchTag = r.Branch
		cs.BranchBG = r.Background
		cs.BranchFG = r.Foreground
		cs.BranchAt = e.Now
		cs.BranchIsWorktree = r.IsWorktree
		cs.BranchParentBranch = r.ParentBranch
		return cs, nil

	case CapturePaneResult:
		cs.CaptureInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		if cs.PaneHash == "" {
			// First capture: prime the baseline, no status change.
			cs.PaneHash = r.Hash
			cs.PaneHashAt = e.Now
			return cs, nil
		}
		if r.Hash != cs.PaneHash {
			cs.PaneHash = r.Hash
			cs.PaneHashAt = e.Now
		}
		return cs, nil
	}
	return cs, nil
}
