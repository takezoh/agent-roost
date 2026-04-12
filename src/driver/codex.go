package driver

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

const (
	CodexDriverName = "codex"

	codexKeyRoostSessionID      = "roost_session_id"
	codexKeyCodexSessionID      = "codex_session_id"
	codexKeyWorkingDir          = "working_dir"
	codexKeyTranscriptPath      = "transcript_path"
	codexKeyStatus              = "status"
	codexKeyStatusChangedAt     = "status_changed_at"
	codexKeyBranchTag           = "branch_tag"
	codexKeyBranchBG            = "branch_bg"
	codexKeyBranchFG            = "branch_fg"
	codexKeyBranchTarget        = "branch_target"
	codexKeyBranchAt            = "branch_at"
	codexKeyBranchIsWorktree    = "branch_is_worktree"
	codexKeyBranchParentBranch  = "branch_parent_branch"
	codexKeyLastPrompt          = "last_prompt"
	codexKeyLastAssistant       = "last_assistant_message"
	codexKeyLastHookEvent       = "last_hook_event"
	codexKeyLastHookAt          = "last_hook_at"
	codexPromptPreviewMaxLength = 80
)

type CodexState struct {
	state.DriverStateBase

	RoostSessionID string
	CodexSessionID string
	WorkingDir     string
	TranscriptPath string

	Status          state.Status
	StatusChangedAt time.Time

	BranchTag          string
	BranchBG           string
	BranchFG           string
	BranchTarget       string
	BranchAt           time.Time
	BranchIsWorktree   bool
	BranchParentBranch string
	BranchInFlight     bool

	LastPrompt           string
	LastAssistantMessage string

	LastHookEvent string
	LastHookAt    time.Time
}

type CodexDriver struct {
	eventLogDir string
}

type codexHookPayload struct {
	SessionID            string         `json:"session_id"`
	HookEventName        string         `json:"hook_event_name"`
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

func (d CodexDriver) View(s state.DriverState) state.View {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	return d.view(cs)
}

func (d CodexDriver) NewState(now time.Time) state.DriverState {
	return CodexState{
		Status:          state.StatusIdle,
		StatusChangedAt: now,
	}
}

func (d CodexDriver) SpawnCommand(s state.DriverState, baseCommand string) string {
	cs, ok := s.(CodexState)
	if !ok || cs.CodexSessionID == "" {
		return baseCommand
	}
	if !isAlphanumHyphen(cs.CodexSessionID) || hasResumeToken(baseCommand) {
		return baseCommand
	}
	fields := strings.Fields(baseCommand)
	if len(fields) == 0 || fields[0] != CodexDriverName {
		return baseCommand
	}
	return strings.TrimSpace(baseCommand) + " resume " + cs.CodexSessionID
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
	json.Unmarshal(payload, &hp)
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
	case state.DEvJobResult:
		next, effs := d.handleJobResult(cs, e)
		return next, effs, d.view(next)
	}
	return cs, nil, d.view(cs)
}
