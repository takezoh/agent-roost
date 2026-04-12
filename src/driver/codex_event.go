package driver

import (
	"fmt"
	"strings"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func (hp codexHookPayload) toolInputString(key string) string {
	if hp.ToolInput == nil {
		return ""
	}
	v, _ := hp.ToolInput[key].(string)
	return v
}

func (hp codexHookPayload) formatLog() string {
	name := hp.HookEventName
	switch hp.HookEventName {
	case "SessionStart":
		if hp.Source != "" {
			return name + " " + hp.Source
		}
	case "UserPromptSubmit":
		if hp.Prompt != "" {
			return fmt.Sprintf(`%s prompt="%s"`, name, previewText(hp.Prompt))
		}
	case "PreToolUse", "PostToolUse":
		name = strings.TrimSpace(name + " " + hp.ToolName)
		if cmd := hp.toolInputString("command"); cmd != "" {
			return fmt.Sprintf(`%s cmd="%s"`, name, previewText(cmd))
		}
		return name
	case "Stop":
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

func statusTime(ts, fallback time.Time) time.Time {
	if !ts.IsZero() {
		return ts
	}
	return fallback
}

func applyHookStatus(cs CodexState, status state.Status, ts time.Time) CodexState {
	cs.Status = status
	cs.StatusChangedAt = statusTime(ts, cs.StatusChangedAt)
	return cs
}

func (d CodexDriver) handleHook(cs CodexState, e state.DEvHook) (CodexState, []state.Effect) {
	hp := parseCodexHookPayload(e.Payload)
	if hp.SessionID == "" {
		return cs, nil
	}
	if !e.Timestamp.IsZero() && !e.Timestamp.After(cs.LastHookAt) {
		return cs, nil
	}

	cs.LastHookEvent = hp.HookEventName
	if !e.Timestamp.IsZero() {
		cs.LastHookAt = e.Timestamp
	}
	if e.RoostSessionID != "" {
		cs.RoostSessionID = e.RoostSessionID
	}
	cs.CodexSessionID = hp.SessionID
	if hp.Cwd != "" {
		cs.WorkingDir = hp.Cwd
	}
	if hp.TranscriptPath != "" {
		cs.TranscriptPath = hp.TranscriptPath
	}

	var effs []state.Effect

	switch hp.HookEventName {
	case "SessionStart":
		cs = applyHookStatus(cs, state.StatusIdle, e.Timestamp)
		target := cs.WorkingDir
		if target != "" && !cs.BranchInFlight {
			cs.BranchInFlight = true
			cs.BranchTarget = target
			effs = append(effs, state.EffStartJob{
				Input: BranchDetectInput{WorkingDir: target},
			})
		}
	case "UserPromptSubmit":
		cs.LastPrompt = strings.TrimSpace(hp.Prompt)
		cs = applyHookStatus(cs, state.StatusRunning, e.Timestamp)
	case "PreToolUse", "PostToolUse":
		cs = applyHookStatus(cs, state.StatusRunning, e.Timestamp)
	case "Stop":
		if msg := strings.TrimSpace(hp.LastAssistantMessage); msg != "" {
			cs.LastAssistantMessage = msg
		}
		cs = applyHookStatus(cs, state.StatusWaiting, e.Timestamp)
	}

	line := strings.TrimSpace(hp.formatLog())
	if line != "" {
		effs = append(effs, state.EffEventLogAppend{Line: line})
	}
	return cs, effs
}

func (d CodexDriver) handleJobResult(cs CodexState, e state.DEvJobResult) (CodexState, []state.Effect) {
	switch r := e.Result.(type) {
	case BranchDetectResult:
		cs.BranchInFlight = false
		if e.Err != nil || r.Branch == "" {
			return cs, nil
		}
		cs.BranchTag = r.Branch
		cs.BranchBG = r.Background
		cs.BranchFG = r.Foreground
		cs.BranchAt = e.Now
		cs.BranchIsWorktree = r.IsWorktree
		cs.BranchParentBranch = r.ParentBranch
	}
	return cs, nil
}
