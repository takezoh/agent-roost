package driver

import (
	"time"

	"github.com/takezoh/agent-roost/state"
)

func (CodexDriver) Persist(s state.DriverState) map[string]string {
	cs, ok := s.(CodexState)
	if !ok {
		return nil
	}
	out := map[string]string{
		codexKeyStatus: cs.Status.String(),
	}
	if cs.RoostSessionID != "" {
		out[codexKeyRoostSessionID] = cs.RoostSessionID
	}
	if cs.CodexSessionID != "" {
		out[codexKeyCodexSessionID] = cs.CodexSessionID
	}
	if cs.WorkingDir != "" {
		out[codexKeyWorkingDir] = cs.WorkingDir
	}
	if cs.TranscriptPath != "" {
		out[codexKeyTranscriptPath] = cs.TranscriptPath
	}
	if !cs.StatusChangedAt.IsZero() {
		out[codexKeyStatusChangedAt] = cs.StatusChangedAt.UTC().Format(time.RFC3339)
	}
	if cs.BranchTag != "" {
		out[codexKeyBranchTag] = cs.BranchTag
	}
	if cs.BranchBG != "" {
		out[codexKeyBranchBG] = cs.BranchBG
	}
	if cs.BranchFG != "" {
		out[codexKeyBranchFG] = cs.BranchFG
	}
	if cs.BranchTarget != "" {
		out[codexKeyBranchTarget] = cs.BranchTarget
	}
	if !cs.BranchAt.IsZero() {
		out[codexKeyBranchAt] = cs.BranchAt.UTC().Format(time.RFC3339)
	}
	if cs.BranchIsWorktree {
		out[codexKeyBranchIsWorktree] = "1"
	}
	if cs.BranchParentBranch != "" {
		out[codexKeyBranchParentBranch] = cs.BranchParentBranch
	}
	if cs.LastPrompt != "" {
		out[codexKeyLastPrompt] = cs.LastPrompt
	}
	if cs.LastAssistantMessage != "" {
		out[codexKeyLastAssistant] = cs.LastAssistantMessage
	}
	if cs.LastHookEvent != "" {
		out[codexKeyLastHookEvent] = cs.LastHookEvent
	}
	if !cs.LastHookAt.IsZero() {
		out[codexKeyLastHookAt] = cs.LastHookAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (d CodexDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	cs := CodexState{
		Status:          state.StatusIdle,
		StatusChangedAt: now,
	}
	if len(bag) == 0 {
		return cs
	}
	cs.RoostSessionID = bag[codexKeyRoostSessionID]
	cs.CodexSessionID = bag[codexKeyCodexSessionID]
	cs.WorkingDir = bag[codexKeyWorkingDir]
	cs.TranscriptPath = bag[codexKeyTranscriptPath]
	if v := bag[codexKeyStatus]; v != "" {
		if st, ok := state.ParseStatus(v); ok {
			cs.Status = st
		}
	}
	if v := bag[codexKeyStatusChangedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cs.StatusChangedAt = t
		}
	}
	cs.BranchTag = bag[codexKeyBranchTag]
	cs.BranchBG = bag[codexKeyBranchBG]
	cs.BranchFG = bag[codexKeyBranchFG]
	cs.BranchTarget = bag[codexKeyBranchTarget]
	if v := bag[codexKeyBranchAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cs.BranchAt = t
		}
	}
	cs.BranchIsWorktree = bag[codexKeyBranchIsWorktree] == "1"
	cs.BranchParentBranch = bag[codexKeyBranchParentBranch]
	cs.LastPrompt = bag[codexKeyLastPrompt]
	cs.LastAssistantMessage = bag[codexKeyLastAssistant]
	cs.LastHookEvent = bag[codexKeyLastHookEvent]
	if v := bag[codexKeyLastHookAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cs.LastHookAt = t
		}
	}
	return cs
}
