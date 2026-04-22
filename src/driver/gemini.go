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

	GeminiSessionID   string
	ManagedWorkingDir string // set when roost pre-created the git worktree
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
	return gs.StartDir
}

func (GeminiDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	gs, ok := s.(GeminiState)
	if !ok {
		return s
	}
	gs.StartDir = dir
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
	command := stripped
	if gs.ManagedWorkingDir == "" {
		command = appendFlag(stripped, "--worktree", req.Enabled)
	}
	if mode != state.LaunchModeColdStart || gs.GeminiSessionID == "" || !isAlphanumHyphen(gs.GeminiSessionID) {
		return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
	}
	command = strings.TrimSpace(stripGeminiWorktreeFlag(command))
	if strings.Contains(command, "--resume") || strings.Contains(command, " -r") {
		return state.LaunchPlan{Command: command, StartDir: startDir, Stdin: options.InitialInput}, nil
	}
	return state.LaunchPlan{
		Command:  command + " --resume " + gs.GeminiSessionID,
		StartDir: startDir,
		Options:  state.LaunchOptions{},
		Stdin:    options.InitialInput,
	}, nil
}

func stripGeminiWorktreeFlag(command string) string {
	_, stripped := parseWorktreeFlags(command, "--worktree", "--workspace")
	return stripped
}

func (d GeminiDriver) Step(prev state.DriverState, ctx state.FrameContext, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	gs, ok := prev.(GeminiState)
	if !ok {
		gs = d.NewState(time.Time{}).(GeminiState)
	}

	switch e := ev.(type) {
	case state.DEvHook:
		next, effs := d.handleHook(gs, ctx, e)
		return next, effs, d.view(next)
	case state.DEvTick:
		if !ctx.IsRoot {
			return gs, nil, d.view(gs)
		}
		effs := gs.HandleTick(e, false)
		return gs, effs, d.view(gs)
	case state.DEvPaneActivity:
		if !ctx.IsRoot {
			return gs, nil, d.view(gs)
		}
		effs := gs.HandleActivity(e)
		return gs, effs, d.view(gs)
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
	_ = json.Unmarshal(payload, &hp)
	return hp
}
