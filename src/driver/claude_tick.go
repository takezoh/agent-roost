package driver

import "github.com/takezoh/agent-roost/state"

// Tick / fsnotify / job-result handling for the Claude driver.

// handleTick is fired by the global ticker.
func (d ClaudeDriver) handleTick(cs ClaudeState, e state.DEvTick) (ClaudeState, []state.Effect) {
	effs := cs.HandleTick(e, hasActiveSubagents(cs))
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
	path := cs.TranscriptPath
	if path == "" {
		path = e.Path
	}
	if path == "" {
		return cs, nil
	}
	var effs []state.Effect
	if cs.WatchedFile != path {
		cs.WatchedFile = path
		effs = append(effs, state.EffWatchFile{Path: path, Kind: "transcript"})
	}
	if cs.TranscriptInFlight {
		return cs, effs
	}
	cs.TranscriptInFlight = true
	effs = append(effs, state.EffStartJob{
		Input: TranscriptParseInput{
			ClaudeUUID: cs.ClaudeSessionID,
			Path:       path,
		},
	})
	return cs, effs
}

// handleJobResult routes a finished worker pool result back to the
// matching field on ClaudeState.
func (d ClaudeDriver) handleJobResult(cs ClaudeState, e state.DEvJobResult) (ClaudeState, []state.Effect) {
	if summary, inFlight, ok := applySummaryJobResult(cs.Summary, cs.SummaryInFlight, e); ok {
		cs.Summary = summary
		cs.SummaryInFlight = inFlight
		return cs, nil
	}

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
		cs.RecentTurns = r.RecentTurns
		cs.PlanFile = r.PlanFile
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
		return cs, cs.HandleCapturePaneResult(r, e.Err, e.Now)
	}
	return cs, nil
}
