package driver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

const (
	CodexDriverName = "codex"

	codexKeyCodexSessionID      = "codex_session_id"
	codexKeyManagedWorkingDir   = "managed_working_dir"
	codexPromptPreviewMaxLength = 80
)

type CodexState struct {
	CommonState

	CodexSessionID     string
	ManagedWorkingDir  string
	TranscriptInFlight bool
	WatchedFile        string
	StatusLine         string
	RecentTurns        []SummaryTurn
}

type CodexDriver struct {
	eventLogDir string
}

type codexHookPayload struct {
	SessionID            string         `json:"session_id"`
	HookEventName        string         `json:"hook_event_name"`
	NotificationType     string         `json:"notification_type"`
	Cwd                  string         `json:"cwd"`
	TranscriptPath       string         `json:"transcript_path"`
	Source               string         `json:"source"`
	Prompt               string         `json:"prompt"`
	ToolName             string         `json:"tool_name"`
	ToolInput            map[string]any `json:"tool_input"`
	LastAssistantMessage string         `json:"last_assistant_message"`
	StopReason           string         `json:"stop_reason"`
}

func NewCodexDriver(eventLogDir string) CodexDriver {
	return CodexDriver{eventLogDir: eventLogDir}
}

func (CodexDriver) Name() string                            { return CodexDriverName }
func (CodexDriver) DisplayName() string                     { return CodexDriverName }
func (CodexDriver) Status(s state.DriverState) state.Status { return s.(CodexState).Status }

func (CodexDriver) StartDir(s state.DriverState) string {
	cs, ok := s.(CodexState)
	if !ok {
		return ""
	}
	return cs.StartDir
}

func (CodexDriver) WithStartDir(s state.DriverState, dir string) state.DriverState {
	cs, ok := s.(CodexState)
	if !ok {
		return s
	}
	cs.StartDir = dir
	return cs
}

func (d CodexDriver) View(s state.DriverState) state.View {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	return d.view(cs)
}

func (d CodexDriver) NewState(now time.Time) state.DriverState {
	return CodexState{
		CommonState: CommonState{
			Status:          state.StatusIdle,
			StatusChangedAt: now,
		},
	}
}

func (d CodexDriver) PrepareLaunch(s state.DriverState, mode state.LaunchMode, project, baseCommand string, options state.LaunchOptions) (state.LaunchPlan, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	startDir := project
	if cs.StartDir != "" {
		startDir = cs.StartDir
	}
	req, stripped := resolveWorktreeRequest(baseCommand, options, "--worktree")
	fields := strings.Fields(stripped)
	if len(fields) == 0 || fields[0] != CodexDriverName {
		return state.LaunchPlan{Command: strings.TrimSpace(baseCommand), StartDir: startDir, Options: options, Stdin: options.InitialInput}, nil
	}
	base := strings.TrimSpace(baseCommand)
	if mode == state.LaunchModeCreate || req.Enabled || cs.ManagedWorkingDir != "" {
		base = stripped
	}
	if mode != state.LaunchModeColdStart || cs.CodexSessionID == "" || !isAlphanumHyphen(cs.CodexSessionID) || hasResumeToken(base) {
		return state.LaunchPlan{
			Command:  base,
			StartDir: startDir,
			Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: req.Enabled || cs.ManagedWorkingDir != ""}},
			Stdin:    options.InitialInput,
		}, nil
	}
	return state.LaunchPlan{
		Command:  strings.TrimSpace(base) + " resume " + cs.CodexSessionID,
		StartDir: startDir,
		Options:  state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: req.Enabled || cs.ManagedWorkingDir != ""}},
		Stdin:    options.InitialInput,
	}, nil
}

func hasResumeToken(command string) bool {
	for _, p := range strings.Fields(command) {
		if p == "resume" {
			return true
		}
	}
	return false
}

func parseCodexHookPayload(payload json.RawMessage) codexHookPayload {
	if len(payload) == 0 {
		return codexHookPayload{}
	}
	var hp codexHookPayload
	_ = json.Unmarshal(payload, &hp)
	return hp
}

func (d CodexDriver) Step(prev state.DriverState, ev state.DriverEvent) (state.DriverState, []state.Effect, state.View) {
	cs, ok := prev.(CodexState)
	if !ok {
		cs = d.NewState(time.Time{}).(CodexState)
	}

	switch e := ev.(type) {
	case state.DEvHook:
		next, effs := d.handleHook(cs, e)
		return next, effs, d.view(next)
	case state.DEvTick:
		effs := cs.HandleTick(e, false)
		return cs, effs, d.view(cs)
	case state.DEvPaneActivity:
		effs := cs.HandleActivity(e)
		return cs, effs, d.view(cs)
	case state.DEvFileChanged:
		next, effs := d.handleTranscriptChanged(cs, e)
		return next, effs, d.view(next)
	case state.DEvJobResult:
		next, effs := d.handleJobResult(cs, e)
		return next, effs, d.view(next)
	}
	return cs, nil, d.view(cs)
}

func (d CodexDriver) WarmStartRecover(s state.DriverState, now time.Time) (state.DriverState, []state.Effect) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = d.NewState(now).(CodexState)
	}
	effs := watchCodexTranscript(&cs)
	effs = append(effs, d.startCodexTranscriptParse(&cs)...)
	return cs, effs
}
