package driver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

const (
	GeminiDriverName = "gemini"

	geminiKeyGeminiSessionID     = "gemini_session_id"
	geminiPromptPreviewMaxLength = 80
)

type GeminiState struct {
	CommonState

	GeminiSessionID string
}

type GeminiDriver struct {
	eventLogDir string
}

func NewGeminiDriver(eventLogDir string) GeminiDriver {
	return GeminiDriver{eventLogDir: eventLogDir}
}

func (GeminiDriver) Name() string                            { return GeminiDriverName }
func (GeminiDriver) DisplayName() string                     { return GeminiDriverName }
func (GeminiDriver) Status(s state.DriverState) state.Status { return s.(GeminiState).Status }

func (d GeminiDriver) View(s state.DriverState) state.View {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	return d.view(gs)
}

func (d GeminiDriver) NewState(now time.Time) state.DriverState {
	return GeminiState{
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
}

func (d GeminiDriver) SpawnCommand(s state.DriverState, baseCommand string) string {
	gs, ok := s.(GeminiState)
	if !ok || gs.GeminiSessionID == "" || !isAlphanumHyphen(gs.GeminiSessionID) {
		return baseCommand
	}
	if strings.Contains(baseCommand, "--resume") || strings.Contains(baseCommand, " -r") {
		return baseCommand
	}
	stripped := stripGeminiWorktreeFlag(baseCommand)
	return strings.TrimSpace(stripped) + " --resume " + gs.GeminiSessionID
}

func stripGeminiWorktreeFlag(command string) string {
	_, stripped := parseWorktreeFlags(command, "--worktree", "--workspace")
	return stripped
}

func (d GeminiDriver) Step(prev state.DriverState, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	gs, ok := prev.(GeminiState)
	if !ok {
		gs = d.NewState(time.Time{}).(GeminiState)
	}

	switch e := ev.(type) {
	case state.DEvHook:
		next, effs := d.handleHook(gs, e)
		return next, effs, d.view(next)
	case state.DEvJobResult:
		next, effs := d.handleJobResult(gs, e)
		return next, effs, d.view(next)
	}
	return gs, nil, d.view(gs)
}

func parseGeminiHookPayload(payload json.RawMessage) geminiHookPayload {
	if len(payload) == 0 {
		return geminiHookPayload{}
	}
	var hp geminiHookPayload
	json.Unmarshal(payload, &hp)
	return hp
}
