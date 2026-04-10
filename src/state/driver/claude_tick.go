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
			Kind:  state.JobGitBranch,
			Input: GitBranchInput{WorkingDir: target},
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
			Kind: state.JobTranscriptParse,
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
	switch e.Kind {
	case state.JobTranscriptParse:
		cs.TranscriptInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		result, ok := e.Result.(TranscriptParseResult)
		if !ok {
			return cs, nil
		}
		// Only overwrite LastPrompt when the parser actually found a
		// user entry — otherwise we'd erase a prompt seeded from
		// UserPromptSubmit before the JSONL flush.
		if result.Title != "" {
			cs.Title = result.Title
		}
		if result.LastPrompt != "" {
			cs.LastPrompt = result.LastPrompt
		}
		cs.StatusLine = result.StatusLine
		cs.CurrentTool = result.CurrentTool
		cs.SubagentCounts = result.Subagents
		return cs, nil

	case state.JobHaikuSummary:
		cs.SummaryInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		result, ok := e.Result.(HaikuSummaryResult)
		if !ok {
			return cs, nil
		}
		if result.Summary != "" {
			cs.Summary = result.Summary
		}
		return cs, nil

	case state.JobGitBranch:
		cs.BranchInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		result, ok := e.Result.(GitBranchResult)
		if !ok {
			return cs, nil
		}
		cs.BranchTag = result.Branch
		cs.BranchAt = e.Now
		return cs, nil
	}
	return cs, nil
}
