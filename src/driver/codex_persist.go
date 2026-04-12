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
	out := make(map[string]string, 10)
	cs.PersistCommon(out)
	if cs.CodexSessionID != "" {
		out[codexKeyCodexSessionID] = cs.CodexSessionID
	}
	if cs.ManagedWorkingDir != "" {
		out[codexKeyManagedWorkingDir] = cs.ManagedWorkingDir
	}
	return out
}

func (d CodexDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	cs := CodexState{
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
	if len(bag) == 0 {
		return cs
	}
	cs.RestoreCommon(bag)
	cs.CodexSessionID = bag[codexKeyCodexSessionID]
	cs.ManagedWorkingDir = bag[codexKeyManagedWorkingDir]
	return cs
}
