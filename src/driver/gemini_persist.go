package driver

import (
	"time"

	"github.com/takezoh/agent-roost/state"
)

func (GeminiDriver) Persist(s state.DriverState) map[string]string {
	gs, ok := s.(GeminiState)
	if !ok {
		return nil
	}
	out := map[string]string{
		geminiKeyStatus: gs.Status.String(),
	}
	if gs.RoostSessionID != "" {
		out[geminiKeyRoostSessionID] = gs.RoostSessionID
	}
	if gs.GeminiSessionID != "" {
		out[geminiKeyGeminiSessionID] = gs.GeminiSessionID
	}
	if gs.WorkingDir != "" {
		out[geminiKeyWorkingDir] = gs.WorkingDir
	}
	if gs.ManagedWorkingDir != "" {
		out[geminiKeyManagedWorkingDir] = gs.ManagedWorkingDir
	}
	if gs.TranscriptPath != "" {
		out[geminiKeyTranscriptPath] = gs.TranscriptPath
	}
	if gs.WorktreeName != "" {
		out[geminiKeyWorktreeName] = gs.WorktreeName
	}
	if !gs.StatusChangedAt.IsZero() {
		out[geminiKeyStatusChangedAt] = gs.StatusChangedAt.UTC().Format(time.RFC3339)
	}
	if gs.BranchTag != "" {
		out[geminiKeyBranchTag] = gs.BranchTag
	}
	if gs.BranchBG != "" {
		out[geminiKeyBranchBG] = gs.BranchBG
	}
	if gs.BranchFG != "" {
		out[geminiKeyBranchFG] = gs.BranchFG
	}
	if gs.BranchTarget != "" {
		out[geminiKeyBranchTarget] = gs.BranchTarget
	}
	if !gs.BranchAt.IsZero() {
		out[geminiKeyBranchAt] = gs.BranchAt.UTC().Format(time.RFC3339)
	}
	if gs.BranchIsWorktree {
		out[geminiKeyBranchIsWorktree] = "1"
	}
	if gs.BranchParentBranch != "" {
		out[geminiKeyBranchParentBranch] = gs.BranchParentBranch
	}
	if gs.LastPrompt != "" {
		out[geminiKeyLastPrompt] = gs.LastPrompt
	}
	if gs.LastAssistantMessage != "" {
		out[geminiKeyLastAssistant] = gs.LastAssistantMessage
	}
	if gs.LastHookEvent != "" {
		out[geminiKeyLastHookEvent] = gs.LastHookEvent
	}
	if !gs.LastHookAt.IsZero() {
		out[geminiKeyLastHookAt] = gs.LastHookAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (d GeminiDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	gs := GeminiState{
		Status:          state.StatusIdle,
		StatusChangedAt: now,
	}
	if len(bag) == 0 {
		return gs
	}
	gs.RoostSessionID = bag[geminiKeyRoostSessionID]
	gs.GeminiSessionID = bag[geminiKeyGeminiSessionID]
	gs.WorkingDir = bag[geminiKeyWorkingDir]
	gs.ManagedWorkingDir = bag[geminiKeyManagedWorkingDir]
	gs.TranscriptPath = bag[geminiKeyTranscriptPath]
	gs.WorktreeName = bag[geminiKeyWorktreeName]
	if v := bag[geminiKeyStatus]; v != "" {
		if st, ok := state.ParseStatus(v); ok {
			gs.Status = st
		}
	}
	if v := bag[geminiKeyStatusChangedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			gs.StatusChangedAt = t
		}
	}
	gs.BranchTag = bag[geminiKeyBranchTag]
	gs.BranchBG = bag[geminiKeyBranchBG]
	gs.BranchFG = bag[geminiKeyBranchFG]
	gs.BranchTarget = bag[geminiKeyBranchTarget]
	if v := bag[geminiKeyBranchAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			gs.BranchAt = t
		}
	}
	gs.BranchIsWorktree = bag[geminiKeyBranchIsWorktree] == "1"
	gs.BranchParentBranch = bag[geminiKeyBranchParentBranch]
	gs.LastPrompt = bag[geminiKeyLastPrompt]
	gs.LastAssistantMessage = bag[geminiKeyLastAssistant]
	gs.LastHookEvent = bag[geminiKeyLastHookEvent]
	if v := bag[geminiKeyLastHookAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			gs.LastHookAt = t
		}
	}
	return gs
}
