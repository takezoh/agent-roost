package driver

import (
	"time"

	"github.com/take/agent-roost/state"
)

// Persist writes the Claude driver state into the bag SessionService
// round-trips through sessions.json. Status is persisted so warm /
// cold restart restores the prior status without resetting to Idle,
// plus the cached branch tag so the user sees the prior branch
// immediately on restart, plus the rolling haiku summary.
func (ClaudeDriver) Persist(s state.DriverState) map[string]string {
	cs, ok := s.(ClaudeState)
	if !ok {
		return nil
	}
	out := make(map[string]string, 9)
	if cs.ClaudeSessionID != "" {
		out[claudeKeyClaudeSessionID] = cs.ClaudeSessionID
	}
	if cs.WorkingDir != "" {
		out[claudeKeyWorkingDir] = cs.WorkingDir
	}
	if cs.TranscriptPath != "" {
		out[claudeKeyTranscriptPath] = cs.TranscriptPath
	}
	out[claudeKeyStatus] = cs.Status.String()
	if !cs.StatusChangedAt.IsZero() {
		out[claudeKeyStatusChangedAt] = cs.StatusChangedAt.UTC().Format(time.RFC3339)
	}
	if cs.BranchTag != "" {
		out[claudeKeyBranchTag] = cs.BranchTag
	}
	if cs.BranchTarget != "" {
		out[claudeKeyBranchTarget] = cs.BranchTarget
	}
	if !cs.BranchAt.IsZero() {
		out[claudeKeyBranchAt] = cs.BranchAt.UTC().Format(time.RFC3339)
	}
	if cs.Summary != "" {
		out[claudeKeySummary] = cs.Summary
	}
	return out
}

// Restore rehydrates ClaudeState from a persisted bag. Empty bags
// produce a fresh state stamped with `now`.
func (d ClaudeDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	cs := ClaudeState{
		Status:          state.StatusIdle,
		StatusChangedAt: now,
	}
	if len(bag) == 0 {
		return cs
	}
	cs.ClaudeSessionID = bag[claudeKeyClaudeSessionID]
	cs.WorkingDir = bag[claudeKeyWorkingDir]
	cs.TranscriptPath = bag[claudeKeyTranscriptPath]
	if v := bag[claudeKeyStatus]; v != "" {
		if status, ok := state.ParseStatus(v); ok {
			cs.Status = status
		}
	}
	if v := bag[claudeKeyStatusChangedAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cs.StatusChangedAt = t
		}
	}
	cs.BranchTag = bag[claudeKeyBranchTag]
	cs.BranchTarget = bag[claudeKeyBranchTarget]
	if v := bag[claudeKeyBranchAt]; v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cs.BranchAt = t
		}
	}
	cs.Summary = bag[claudeKeySummary]
	return cs
}
