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

func (GeminiDriver) StartDir(s state.DriverState) string {
	gs, ok := s.(GeminiState)
	if !ok {
		return ""
	}
	return gs.CommonState.StartDir
}

func (GeminiDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	gs, ok := s.(GeminiState)
	if !ok {
		return s
	}
	gs.CommonState.StartDir = dir
	return gs
}

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

func (d GeminiDriver) PrepareLaunch(s state.DriverState, mode state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	startDir := project
	if gs.StartDir != "" {
		startDir = gs.StartDir
	}
	req, stripped := resolveWorktreeRequest(baseCommand, options, "--worktree", "--workspace")
	command := appendFlag(stripped, "--worktree", req.Enabled)
	if mode != state.LaunchModeColdStart || gs.GeminiSessionID == "" || !isAlphanumHyphen(gs.GeminiSessionID) {
		return state.LaunchPlan{Command: command, StartDir: startDir}, nil
	}
	command = strings.TrimSpace(stripGeminiWorktreeFlag(command))
	if strings.Contains(command, "--resume") || strings.Contains(command, " -r") {
		return state.LaunchPlan{Command: command, StartDir: startDir}, nil
	}
	return state.LaunchPlan{
		Command:  command + " --resume " + gs.GeminiSessionID,
		StartDir: startDir,
		Options:  state.LaunchOptions{},
	}, nil
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
