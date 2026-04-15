package driver

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// Hook event handling for the Claude driver. The hook bridge sends the
// raw JSON payload via DEvHook{Event: hookEventName, Payload: {"raw": ...}}.
// This file parses the raw JSON into a hookPayload struct and dispatches
// by hook_event_name. All field extraction lives here — the bridge is
// a thin relay.

// hookPayload is the minimal subset of the Claude hook JSON the driver
// needs. Parsed from the "raw" key in DEvHook.Payload. Defined here
// (not in lib/claude/hookevent) so state/driver stays a leaf package.
type hookPayload struct {
	SessionID      string `json:"session_id"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
	Cwd            string `json:"cwd"`
	TranscriptPath string `json:"transcript_path"`

	NotificationType string         `json:"notification_type"`
	ToolName         string         `json:"tool_name"`
	ToolInput        map[string]any `json:"tool_input"`
	Source           string         `json:"source"`
}

func (hp hookPayload) toolInputString(key string) string {
	if hp.ToolInput == nil {
		return ""
	}
	v, _ := hp.ToolInput[key].(string)
	return v
}

// deriveState maps the hook_event_name to a roost status string.
// Must stay in sync with lib/claude/hookevent.HookEvent.DeriveState.
func (hp hookPayload) deriveState() string {
	switch hp.HookEventName {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse":
		return "running"
	case "Stop", "StopFailure":
		return "waiting"
	case "SessionEnd":
		return "stopped"
	case "SessionStart":
		return "idle"
	case "Notification":
		switch hp.NotificationType {
		case "permission_prompt":
			return "pending"
		case "idle_prompt", "elicitation_dialog":
			return "waiting"
		}
	}
	return ""
}

// formatLog mirrors lib/claude/hookevent.HookEvent.FormatLog.
func (hp hookPayload) formatLog() string {
	s := hp.HookEventName
	switch hp.HookEventName {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		if hp.ToolName == "" {
			break
		}
		s += " " + hp.ToolName
		if hp.ToolName == "Bash" {
			if cmd := hp.toolInputString("command"); cmd != "" {
				if len(cmd) > 80 {
					cmd = cmd[:77] + "..."
				}
				s += " " + cmd
			}
		} else if hp.ToolName == "Read" || hp.ToolName == "Write" || hp.ToolName == "Edit" || hp.ToolName == "Glob" {
			if fp := hp.toolInputString("file_path"); fp != "" {
				s += " " + fp
			} else if p := hp.toolInputString("pattern"); p != "" {
				s += " " + p
			}
		}
	case "Notification":
		if hp.NotificationType != "" {
			s += " " + hp.NotificationType
		}
	case "SessionStart":
		if hp.Source != "" {
			s += " " + hp.Source
		}
	}
	return s
}

func (hp hookPayload) logEffects() []state.Effect {
	if line := hp.formatLog(); line != "" {
		return []state.Effect{state.EffEventLogAppend{Line: line}}
	}
	return nil
}

func parseHookPayload(payload json.RawMessage) hookPayload {
	if len(payload) == 0 {
		return hookPayload{}
	}
	var hp hookPayload
	json.Unmarshal(payload, &hp)
	return hp
}

// handleHook parses the raw JSON from the bridge and dispatches by
// hook_event_name.
func (d ClaudeDriver) handleHook(cs ClaudeState, e state.DEvHook) (ClaudeState, []state.Effect) {
	hp := parseHookPayload(e.Payload)
	if hp.SessionID == "" {
		return cs, nil
	}

	if e.RoostSessionID != "" {
		cs.RoostSessionID = e.RoostSessionID
	}

	ts := e.Timestamp

	if hp.HookEventName == "SessionStart" {
		cs.LastBridgeTS = ts
		cs.ResetHangDetection()
		return d.handleSessionStart(cs, hp, ts)
	}

	if !ts.IsZero() && !ts.After(cs.LastBridgeTS) {
		slog.Warn("claude: dropping out-of-order hook",
			"event", hp.HookEventName, "ts", ts, "last", cs.LastBridgeTS)
		return cs, nil
	}
	if !ts.IsZero() {
		cs.LastBridgeTS = ts
	}

	// A hook arriving (non-stale) means the agent is alive — clear
	// hang detection state so the timer restarts from scratch.
	cs.ResetHangDetection()

	if hp.HookEventName == "UserPromptSubmit" {
		return d.handleUserPromptSubmit(cs, hp, e.Timestamp)
	}

	// Agent tool events track subagent lifecycle, not main-agent
	// activity — log only, no status change.
	if hp.ToolName == "Agent" {
		return cs, hp.logEffects()
	}

	switch hp.HookEventName {
	case "SubagentStart", "SubagentStop":
		return cs, hp.logEffects()
	}

	// All other hook events (PreToolUse, PostToolUse, Stop, etc.)
	// go through the state-change path if they map to a status.
	status := hp.deriveState()
	if status == "" {
		return cs, hp.logEffects()
	}

	return d.handleStateChange(cs, hp, status, e.Timestamp)
}

// handleSessionStart absorbs identity and kicks initial transcript
// watch + parse + event log.
func (d ClaudeDriver) handleSessionStart(cs ClaudeState, hp hookPayload, now time.Time) (ClaudeState, []state.Effect) {
	cs = absorbIdentityFromHP(cs, hp)
	if hp.Cwd != "" {
		cs.StartDir = hp.Cwd
	}
	if now.IsZero() {
		now = cs.StatusChangedAt
	}
	// Reset to Idle. A SessionStart fires on fresh launch, --resume,
	// /resume, and /clear. In every case the session is freshly
	// initialized. This also clears the Stopped that a preceding
	// SessionEnd wrote — without it a resumed session would stick at
	// Stopped until the user typed something.
	cs.Status = state.StatusIdle
	cs.StatusChangedAt = now

	var effs []state.Effect
	if path := d.resolveTranscriptPath(cs); path != "" && cs.WatchedFile != path {
		cs.WatchedFile = path
		effs = append(effs, state.EffWatchFile{Path: path, Kind: "transcript"})
		if !cs.TranscriptInFlight {
			cs.TranscriptInFlight = true
			effs = append(effs, state.EffStartJob{
				Input: TranscriptParseInput{
					ClaudeUUID: cs.ClaudeSessionID,
					Path:       path,
				},
			})
		}
	}
	effs = append(effs, state.EffEventLogAppend{Line: "SessionStart"})

	// Trigger branch detection immediately so the tag appears before
	// the user types anything (Idle sessions are skipped by tick).
	target := cs.StartDir
	if target != "" && !cs.BranchInFlight {
		cs.BranchInFlight = true
		cs.BranchTarget = target
		effs = append(effs, state.EffStartJob{
			Input: BranchDetectInput{WorkingDir: target},
		})
	}

	return cs, effs
}

// handleStateChange advances the status machine and emits an event log.
func (d ClaudeDriver) handleStateChange(cs ClaudeState, hp hookPayload, statusStr string, now time.Time) (ClaudeState, []state.Effect) {
	cs = absorbIdentityFromHP(cs, hp)
	if now.IsZero() {
		now = cs.StatusChangedAt
	}

	if status, ok := state.ParseStatus(statusStr); ok {
		cs.Status = status
		cs.StatusChangedAt = now
	}

	var effs []state.Effect
	logLine := hp.formatLog()
	if logLine != "" {
		effs = append(effs, state.EffEventLogAppend{Line: logLine})
	}

	if !cs.TranscriptInFlight {
		if path := d.resolveTranscriptPath(cs); path != "" {
			cs.TranscriptInFlight = true
			effs = append(effs, state.EffStartJob{
				Input: TranscriptParseInput{
					ClaudeUUID: cs.ClaudeSessionID,
					Path:       path,
				},
			})
		}
	}

	return cs, effs
}

// handleUserPromptSubmit seeds LastPrompt, triggers haiku summary,
// and also runs the state-change logic (UserPromptSubmit → "running").
func (d ClaudeDriver) handleUserPromptSubmit(cs ClaudeState, hp hookPayload, now time.Time) (ClaudeState, []state.Effect) {
	cs = absorbIdentityFromHP(cs, hp)
	if !now.IsZero() {
		cs.StatusChangedAt = now
	}
	if status, ok := state.ParseStatus("running"); ok {
		cs.Status = status
		cs.StatusChangedAt = now
	}

	if hp.Prompt != "" {
		cs.LastPrompt = hp.Prompt
	}

	var effs []state.Effect
	effs = append(effs, state.EffEventLogAppend{Line: "UserPromptSubmit"})

	turns := appendHookPromptTurn(cs.RecentTurns, hp.Prompt)
	prompt := formatSummaryPrompt(cs.Summary, turns)
	effs, cs.SummaryInFlight = enqueueSummaryJob(effs, cs.SummaryInFlight, prompt)

	if !cs.TranscriptInFlight {
		if path := d.resolveTranscriptPath(cs); path != "" {
			cs.TranscriptInFlight = true
			effs = append(effs, state.EffStartJob{
				Input: TranscriptParseInput{
					ClaudeUUID: cs.ClaudeSessionID,
					Path:       path,
				},
			})
		}
	}

	return cs, effs
}

func absorbIdentityFromHP(cs ClaudeState, hp hookPayload) ClaudeState {
	if hp.SessionID != "" {
		cs.ClaudeSessionID = hp.SessionID
	}
	if hp.TranscriptPath != "" {
		cs.TranscriptPath = hp.TranscriptPath
	}
	return cs
}
