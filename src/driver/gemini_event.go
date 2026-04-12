package driver

import (
	"fmt"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

type geminiHookPayload struct {
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

func (hp geminiHookPayload) toolInputString(key string) string {
	if hp.ToolInput == nil {
		return ""
	}
	v, _ := hp.ToolInput[key].(string)
	return v
}

func (hp geminiHookPayload) deriveStatus() (state.Status, bool) {
	switch hp.HookEventName {
	case "BeforeAgent", "BeforeTool", "AfterTool":
		return state.StatusRunning, true
	case "AfterAgent":
		return state.StatusWaiting, true
	case "SessionStart":
		return state.StatusIdle, true
	case "Notification":
		switch hp.NotificationType {
		case "permission_prompt":
			return state.StatusPending, true
		case "idle_prompt", "elicitation_dialog":
			return state.StatusWaiting, true
		}
	}
	return 0, false
}

func (hp geminiHookPayload) formatLog() string {
	name := hp.HookEventName
	switch hp.HookEventName {
	case "SessionStart":
		if hp.Source != "" {
			return name + " " + hp.Source
		}
	case "BeforeAgent":
		if hp.Prompt != "" {
			return fmt.Sprintf(`%s prompt="%s"`, name, previewText(hp.Prompt))
		}
	case "BeforeTool", "AfterTool":
		name = strings.TrimSpace(name + " " + hp.ToolName)
		if cmd := hp.toolInputString("command"); cmd != "" {
			return fmt.Sprintf(`%s cmd="%s"`, name, previewText(cmd))
		}
		if path := hp.toolInputString("file_path"); path != "" {
			return fmt.Sprintf(`%s path="%s"`, name, previewText(path))
		}
		return name
	case "Notification":
		if hp.NotificationType != "" {
			return name + " " + hp.NotificationType
		}
	case "AfterAgent":
		var parts []string
		if hp.StopReason != "" {
			parts = append(parts, "reason="+previewText(hp.StopReason))
		}
		if hp.LastAssistantMessage != "" {
			parts = append(parts, fmt.Sprintf(`last="%s"`, previewText(hp.LastAssistantMessage)))
		}
		if len(parts) > 0 {
			return name + " " + strings.Join(parts, " ")
		}
	}
	return name
}

func (d GeminiDriver) handleHook(gs GeminiState, e state.DEvHook) (GeminiState, []state.Effect) {
	hp := parseGeminiHookPayload(e.Payload)
	if hp.SessionID == "" {
		return gs, nil
	}
	if !e.Timestamp.IsZero() && !e.Timestamp.After(gs.LastHookAt) {
		return gs, nil
	}

	gs.LastHookEvent = hp.HookEventName
	if !e.Timestamp.IsZero() {
		gs.LastHookAt = e.Timestamp
	}
	if e.RoostSessionID != "" {
		gs.RoostSessionID = e.RoostSessionID
	}
	gs.GeminiSessionID = hp.SessionID
	if hp.Cwd != "" {
		gs.WorkingDir = hp.Cwd
	}
	if hp.TranscriptPath != "" {
		gs.TranscriptPath = hp.TranscriptPath
	}

	var effs []state.Effect

	// Update Status
	if status, ok := hp.deriveStatus(); ok {
		gs.Status = status
		gs.StatusChangedAt = statusTime(e.Timestamp, gs.StatusChangedAt)
	}

	// Session Start specific work
	if hp.HookEventName == "SessionStart" {
		target := gs.WorkingDir
		if target != "" && !gs.BranchInFlight {
			gs.BranchInFlight = true
			gs.BranchTarget = target
			effs = append(effs, state.EffStartJob{
				Input: BranchDetectInput{WorkingDir: target},
			})
		}
	}

	// Capture messages for UI subtitle
	if hp.HookEventName == "BeforeAgent" {
		gs.LastPrompt = strings.TrimSpace(hp.Prompt)
	}
	if hp.HookEventName == "AfterAgent" {
		if msg := strings.TrimSpace(hp.LastAssistantMessage); msg != "" {
			gs.LastAssistantMessage = msg
		}
	}

	line := strings.TrimSpace(hp.formatLog())
	if line != "" {
		effs = append(effs, state.EffEventLogAppend{Line: line})
	}
	return gs, effs
}

func (d GeminiDriver) handleJobResult(gs GeminiState, e state.DEvJobResult) (GeminiState, []state.Effect) {
	switch r := e.Result.(type) {
	case BranchDetectResult:
		gs.BranchInFlight = false
		if e.Err != nil || r.Branch == "" {
			return gs, nil
		}
		gs.BranchTag = r.Branch
		gs.BranchBG = r.Background
		gs.BranchFG = r.Foreground
		gs.BranchAt = e.Now
		gs.BranchIsWorktree = r.IsWorktree
		gs.BranchParentBranch = r.ParentBranch
	}
	return gs, nil
}
