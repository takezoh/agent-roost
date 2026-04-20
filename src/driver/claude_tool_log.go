package driver

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/takezoh/agent-roost/state"
)

// toolLogEntry is the per-tool JSONL record written to
// <dataDir>/claude/tool-logs/<project>.jsonl.
type toolLogEntry struct {
	TS              time.Time      `json:"ts"`
	RoostSessionID  string         `json:"roost_session_id,omitempty"`
	ClaudeSessionID string         `json:"claude_session_id,omitempty"`
	ToolUseID       string         `json:"tool_use_id,omitempty"`
	ToolName        string         `json:"tool_name"`
	Kind            string         `json:"kind"` // approved | auto | denied | failed | orphan
	PermissionMode  string         `json:"permission_mode,omitempty"`
	DurationMs      int64          `json:"duration_ms,omitempty"`
	ToolInput       map[string]any `json:"tool_input,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// buildToolLogLine marshals entry to a single JSON line (no trailing
// newline). The backend appends the newline.
func buildToolLogLine(entry toolLogEntry) string {
	b, err := json.Marshal(entry)
	if err != nil {
		// Should never happen; fall back to a minimal record.
		return `{"kind":"` + entry.Kind + `","tool_name":"` + entry.ToolName + `"}`
	}
	return string(b)
}

// summariseToolInput returns a compact representation of tool_input
// suitable for logging. String values are truncated to 200 characters
// to bound log size and reduce the risk of sensitive data at rest.
//
// Tool-specific rules mirror the formatLog field selection so that the
// two log formats remain consistent:
//
//   - Bash: keep "command" and "description"
//   - Read/Write/Edit/Glob/Grep: keep "file_path" and/or "pattern"
//   - Agent: keep "description"
//   - All others: keep entire input (truncating strings)
func summariseToolInput(name string, in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	switch name {
	case "Bash":
		return pickAndTrunc(in, "command", "description")
	case "Read", "Write", "Edit", "Glob":
		return pickAndTrunc(in, "file_path", "pattern")
	case "Grep":
		return pickAndTrunc(in, "pattern", "path", "glob")
	case "Agent":
		return pickAndTrunc(in, "description", "subagent_type")
	default:
		return truncStrings(in)
	}
}

// resolveProjectSlug converts a Session.Project absolute path to the
// slug used as the tool-log filename. Returns "" when project is empty
// (the runtime's validateSlug will reject the empty value).
func resolveProjectSlug(project string) string {
	if project == "" {
		return ""
	}
	return projectDir(project)
}

// pickAndTrunc returns a new map containing only the listed keys whose
// values are non-nil. String values are truncated to 200 chars.
func pickAndTrunc(in map[string]any, keys ...string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		v, ok := in[k]
		if !ok || v == nil {
			continue
		}
		out[k] = truncValue(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// truncStrings returns a shallow copy of m with all string values
// truncated to 200 characters.
func truncStrings(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = truncValue(v)
	}
	return out
}

// handleToolLog handles the tool-log side-channel for PreToolUse,
// PostToolUse, PostToolUseFailure, and permission_prompt Notifications.
// It updates cs.PendingTools and returns any EffToolLogAppend effects.
// Called from handleHook before the generic state-change path so that
// PendingTools mutations are visible to subsequent handlers.
func (d ClaudeDriver) handleToolLog(cs ClaudeState, hp hookPayload, now time.Time) (ClaudeState, []state.Effect) {
	switch hp.HookEventName {
	case "PreToolUse":
		if hp.ToolUseID == "" || hp.ToolName == "" {
			// Old Claude Code or missing fields — skip correlation.
			return cs, nil
		}
		if cs.PendingTools == nil {
			cs.PendingTools = make(map[string]pendingTool)
		}
		cs.PendingTools[hp.ToolUseID] = pendingTool{
			Name:      hp.ToolName,
			Input:     hp.ToolInput,
			StartedAt: now,
			PermMode:  hp.PermissionMode,
		}
		return cs, nil

	case "Notification":
		if hp.NotificationType != "permission_prompt" || len(cs.PendingTools) == 0 {
			return cs, nil
		}
		// Mark the oldest pending tool (by StartedAt) that has not yet
		// been marked. Claude Code shows permission prompts serially, so
		// the oldest pending tool is most likely the one being prompted.
		var oldestID string
		var oldestTS time.Time
		for id, p := range cs.PendingTools {
			if !p.SawPrompt && (oldestID == "" || p.StartedAt.Before(oldestTS)) {
				oldestID = id
				oldestTS = p.StartedAt
			}
		}
		if oldestID != "" {
			entry := cs.PendingTools[oldestID]
			entry.SawPrompt = true
			cs.PendingTools[oldestID] = entry
		}
		return cs, nil

	case "PostToolUse":
		return d.emitToolLog(cs, hp, now, "")

	case "PostToolUseFailure":
		kind := "failed"
		if hp.IsInterrupt {
			kind = "denied"
		}
		return d.emitToolLog(cs, hp, now, kind)
	}
	return cs, nil
}

// emitToolLog looks up the matching PreToolUse entry, derives the kind,
// builds a JSONL line, emits EffToolLogAppend, and removes the entry
// from PendingTools. kindOverride is non-empty only for failure paths.
func (d ClaudeDriver) emitToolLog(cs ClaudeState, hp hookPayload, now time.Time, kindOverride string) (ClaudeState, []state.Effect) { //nolint:funlen
	var (
		kind       string
		durationMs int64
		toolInput  map[string]any
	)

	if hp.ToolUseID == "" {
		// No tool_use_id: old Claude Code. Emit directly without Pre lookup.
		kind = kindOverride
		if kind == "" {
			kind = "auto"
		}
		toolInput = hp.ToolInput
	} else if entry, ok := cs.PendingTools[hp.ToolUseID]; ok {
		delete(cs.PendingTools, hp.ToolUseID)
		kind = kindOverride
		if kind == "" {
			if entry.SawPrompt {
				kind = "approved"
			} else {
				kind = "auto"
			}
		}
		if !entry.StartedAt.IsZero() && !now.IsZero() {
			durationMs = now.Sub(entry.StartedAt).Milliseconds()
		}
		toolInput = entry.Input
	} else {
		// Orphan: Post arrived without a matching Pre (daemon restart etc.)
		slog.Debug("claude: tool log orphan",
			"event", hp.HookEventName, "tool", hp.ToolName, "id", hp.ToolUseID)
		kind = kindOverride
		if kind == "" {
			kind = "orphan"
		}
		toolInput = hp.ToolInput
	}

	slug := resolveProjectSlug(cs.Project)
	if slug == "" {
		return cs, nil
	}

	line := buildToolLogLine(toolLogEntry{
		TS:              now,
		RoostSessionID:  cs.RoostSessionID,
		ClaudeSessionID: cs.ClaudeSessionID,
		ToolUseID:       hp.ToolUseID,
		ToolName:        hp.ToolName,
		Kind:            kind,
		PermissionMode:  hp.PermissionMode,
		DurationMs:      durationMs,
		ToolInput:       summariseToolInput(hp.ToolName, toolInput),
		Error:           hp.Error,
	})

	return cs, []state.Effect{state.EffToolLogAppend{Namespace: "claude", Project: slug, Line: line}}
}

func truncValue(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	const maxLen = 200
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "…"
	}
	return s
}
