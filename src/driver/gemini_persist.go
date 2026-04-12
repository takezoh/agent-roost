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
	out := make(map[string]string, 10)
	gs.PersistCommon(out)
	if gs.GeminiSessionID != "" {
		out[geminiKeyGeminiSessionID] = gs.GeminiSessionID
	}
	return out
}

func (d GeminiDriver) Restore(bag map[string]string, now time.Time) state.DriverState {
	gs := GeminiState{
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
	if len(bag) == 0 {
		return gs
	}
	gs.RestoreCommon(bag)
	gs.GeminiSessionID = bag[geminiKeyGeminiSessionID]
	return gs
}
