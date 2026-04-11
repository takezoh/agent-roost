package driver

import "github.com/take/agent-roost/state"

// Tick / fsnotify / job-result handling for the Claude driver.

// handleTick is fired by the global ticker. The Claude driver is
// event-driven so the only periodic work is the branch tag refresh
// (gated on the session being active so background sessions don't
// spend git CPU on themselves).
func (d ClaudeDriver) handleTick(cs ClaudeState, e state.DEvTick) (ClaudeState, []state.Effect) {
	if !e.Active {
		return cs, nil
	}

	// Branch refresh: only if the cache is stale or the working dir
	// changed since last detection. The job is async; the in-flight
	// flag prevents pile-up.
	target := cs.WorkingDir
	if target == "" {
		target = e.Project
	}
	if target == "" || cs.BranchInFlight {
		return cs, nil
	}
	if target == cs.BranchTarget && e.Now.Sub(cs.BranchAt) < claudeBranchRefreshInterval {
		return cs, nil
	}
	cs.BranchInFlight = true
	cs.BranchTarget = target // record what we asked about so the result can be matched
	return cs, []state.Effect{
		state.EffStartJob{
			Input: BranchDetectInput{WorkingDir: target},
		},
	}
}

// handleTranscriptChanged fires when fsnotify reports a write to the
// active session's transcript file. We schedule an incremental parse
// (deduped via TranscriptInFlight).
func (d ClaudeDriver) handleTranscriptChanged(cs ClaudeState, e state.DEvTranscriptChanged) (ClaudeState, []state.Effect) {
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
		if e.Err != nil {
			return cs, nil
		}
		cs.BranchTag = r.Branch
		cs.BranchBG = r.Background
		cs.BranchFG = r.Foreground
		cs.BranchAt = e.Now
		return cs, nil
	}
	return cs, nil
}
