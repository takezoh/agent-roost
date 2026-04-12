package driver

import (
	"time"

	"github.com/takezoh/agent-roost/state"
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
	out := make(map[string]string, 10)
	cs.PersistCommon(out)
	if cs.ClaudeSessionID != "" {
		out[claudeKeyClaudeSessionID] = cs.ClaudeSessionID
	}
	return out
}

// Restore rehydrates ClaudeState from a persisted bag. Empty bags
// produce a fresh state stamped with `now`.
func (d ClaudeDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	cs := ClaudeState{
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
	if len(bag) == 0 {
		return cs
	}
	cs.RestoreCommon(bag)
	cs.ClaudeSessionID = bag[claudeKeyClaudeSessionID]
	return cs
}
