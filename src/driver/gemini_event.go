package driver

import (
	"fmt"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

type geminiHookPayload struct {
	SessionID        string         `json:"session_id"`
	HookEventName    string         `json:"hook_event_name"`
	NotificationType string         `json:"notification_type"`
	Cwd              string         `json:"cwd"`
	TranscriptPath   string         `json:"transcript_path"`
	Source           string         `json:"source"`
	Prompt           string         `json:"prompt"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
	PromptResponse   string         `json:"prompt_response"`
	StopReason       string         `json:"stop_reason"`
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
		if hp.NotificationType == "ToolPermission" {
			return state.StatusPending, true
		}
	case "SessionEnd":
		return state.StatusStopped, true
	}
	return 0, false
}

func (hp geminiHookPayload) formatLog() string {
	name := hp.HookEventName
	detail := ""
	switch hp.HookEventName {
	case "SessionStart":
		detail = hp.Source
	case "BeforeAgent":
		if hp.Prompt != "" {
			detail = fmt.Sprintf(`prompt="%s"`, previewText(hp.Prompt))
		}
	case "BeforeTool", "AfterTool":
		detail = strings.TrimSpace(hp.ToolName)
		if cmd := hp.toolInputString("command"); cmd != "" {
			detail = strings.TrimSpace(fmt.Sprintf(`%s cmd="%s"`, detail, previewText(cmd)))
		} else if path := hp.toolInputString("file_path"); path != "" {
			detail = strings.TrimSpace(fmt.Sprintf(`%s path="%s"`, detail, previewText(path)))
		}
	case "Notification":
		detail = hp.NotificationType
	case "AfterAgent":
		var parts []string
		if hp.StopReason != "" {
			parts = append(parts, "reason="+previewText(hp.StopReason))
		}
		if hp.PromptResponse != "" {
			parts = append(parts, fmt.Sprintf(`resp="%s"`, previewText(hp.PromptResponse)))
		}
		detail = strings.Join(parts, " ")
	}
	return eventLogLine(name, detail)
}

func (d GeminiDriver) handleHook(gs GeminiState, e state.DEvHook) (GeminiState, []state.Effect) {
	hp := parseGeminiHookPayload(e.Payload)
	if hp.SessionID == "" {
		return gs, nil
	}
	if !e.Timestamp.IsZero() && !e.Timestamp.After(gs.LastHookAt) {
		return gs, nil
	}

	gs.ResetHangDetection()
	gs.LastHookEvent = hp.HookEventName
	if !e.Timestamp.IsZero() {
		gs.LastHookAt = e.Timestamp
	}
	if e.RoostSessionID != "" {
		gs.RoostSessionID = e.RoostSessionID
	}
	gs.GeminiSessionID = hp.SessionID
	if hp.Cwd != "" {
		gs.StartDir = hp.Cwd
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
		target := gs.StartDir
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
		if msg := strings.TrimSpace(hp.PromptResponse); msg != "" {
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
	case CapturePaneResult:
		gs.HandleCapturePaneResult(r, e.Err, e.Now)
	}
	return gs, nil
}
