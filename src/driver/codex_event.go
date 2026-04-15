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
	case "Notification":
		if hp.NotificationType != "" {
			return name + " " + hp.NotificationType
		}
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

	cs.ResetHangDetection()
	cs.LastHookEvent = hp.HookEventName
	if !e.Timestamp.IsZero() {
		cs.LastHookAt = e.Timestamp
	}
	if e.RoostSessionID != "" {
		cs.RoostSessionID = e.RoostSessionID
	}
	cs.CodexSessionID = hp.SessionID
	if hp.Cwd != "" {
		cs.StartDir = hp.Cwd
	}
	if hp.TranscriptPath != "" {
		cs.TranscriptPath = hp.TranscriptPath
	}

	var effs []state.Effect
	effs = append(effs, watchCodexTranscript(&cs)...)

	switch hp.HookEventName {
	case "SessionStart":
		cs = applyHookStatus(cs, state.StatusIdle, e.Timestamp)
		effs = append(effs, d.startCodexTranscriptParse(&cs)...)
		target := cs.StartDir
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
		turns := recentUserTurns(appendHookPromptTurn(cs.RecentTurns, hp.Prompt), 2)
		prompt := formatSummaryPrompt(cs.Summary, turns)
		effs, cs.SummaryInFlight = enqueueSummaryJob(effs, cs.SummaryInFlight, prompt)
		effs = append(effs, d.startCodexTranscriptParse(&cs)...)
	case "PreToolUse", "PostToolUse":
		cs = applyHookStatus(cs, state.StatusRunning, e.Timestamp)
	case "Notification":
		switch hp.NotificationType {
		case "permission_prompt":
			cs = applyHookStatus(cs, state.StatusPending, e.Timestamp)
		case "idle_prompt", "elicitation_dialog":
			cs = applyHookStatus(cs, state.StatusWaiting, e.Timestamp)
		}
	case "Stop":
		if msg := strings.TrimSpace(hp.LastAssistantMessage); msg != "" {
			cs.LastAssistantMessage = msg
		}
		cs = applyHookStatus(cs, state.StatusWaiting, e.Timestamp)
		effs = append(effs, d.startCodexTranscriptParse(&cs)...)
	}

	line := strings.TrimSpace(hp.formatLog())
	if line != "" {
		effs = append(effs, state.EffEventLogAppend{Line: line})
	}
	return cs, effs
}

func (d CodexDriver) handleJobResult(cs CodexState, e state.DEvJobResult) (CodexState, []state.Effect) {
	if summary, inFlight, ok := applySummaryJobResult(cs.Summary, cs.SummaryInFlight, e); ok {
		cs.Summary = summary
		cs.SummaryInFlight = inFlight
		return cs, nil
	}

	switch r := e.Result.(type) {
	case CodexTranscriptParseResult:
		cs.TranscriptInFlight = false
		if e.Err != nil {
			return cs, nil
		}
		if r.Title != "" {
			cs.Title = r.Title
		}
		if r.LastPrompt != "" {
			cs.LastPrompt = r.LastPrompt
		}
		if r.LastAssistantMessage != "" {
			cs.LastAssistantMessage = r.LastAssistantMessage
		}
		cs.StatusLine = r.StatusLine
		cs.RecentTurns = r.RecentTurns
		return cs, nil
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
	case CapturePaneResult:
		cs.HandleCapturePaneResult(r, e.Err, e.Now)
	}
	return cs, nil
}

func (d CodexDriver) handleTranscriptChanged(cs CodexState, e state.DEvFileChanged) (CodexState, []state.Effect) {
	if cs.TranscriptPath != "" && e.Path != "" && cs.TranscriptPath != e.Path {
		return cs, nil
	}
	effs := watchCodexTranscript(&cs)
	effs = append(effs, d.startCodexTranscriptParse(&cs)...)
	return cs, effs
}

func (d CodexDriver) startCodexTranscriptParse(cs *CodexState) []state.Effect {
	if cs.TranscriptInFlight || cs.TranscriptPath == "" {
		return nil
	}
	cs.TranscriptInFlight = true
	return []state.Effect{
		state.EffStartJob{
			Input: CodexTranscriptParseInput{
				Path: cs.TranscriptPath,
			},
		},
	}
}

func watchCodexTranscript(cs *CodexState) []state.Effect {
	if cs.TranscriptPath == "" || cs.WatchedFile == cs.TranscriptPath {
		return nil
	}
	cs.WatchedFile = cs.TranscriptPath
	return []state.Effect{state.EffWatchFile{Path: cs.TranscriptPath, Kind: "transcript"}}
}
