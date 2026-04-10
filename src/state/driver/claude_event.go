package driver

import (
	"time"

	"github.com/take/agent-roost/state"
)

// Hook event handling for the Claude driver. The hook bridge
// (`roost claude event`) parses the raw Claude hook payload, the
// reducer wraps it in a state.DEvHook, and this file dispatches on
// the Event field.
//
// Recognized event names — these are the hook_event_name values from
// Claude Code's hooks JSON, lowercased and translated to the roost
// shape: "session-start", "user-prompt-submit", "state-change",
// "pre-tool-use", "post-tool-use", "stop", "subagent-stop".

const (
	hookSessionStart     = "session-start"
	hookStateChange      = "state-change"
	hookUserPromptSubmit = "user-prompt-submit"
)

// handleHook routes a DEvHook to the right per-event handler. The
// payload is a typed map[string]any owned by the reducer; this file
// is the only place that touches the payload key set.
func (d ClaudeDriver) handleHook(cs ClaudeState, e state.DEvHook) (ClaudeState, []state.Effect) {
	switch e.Event {
	case hookSessionStart:
		return d.handleSessionStart(cs, e.Payload)
	case hookStateChange:
		return d.handleStateChange(cs, e.Payload)
	case hookUserPromptSubmit:
		return d.handleUserPromptSubmit(cs, e.Payload)
	}
	return cs, nil
}

// handleSessionStart absorbs identity keys (claude session id, working
// dir, transcript path) and emits the initial transcript watch +
// parse + event log line.
func (d ClaudeDriver) handleSessionStart(cs ClaudeState, payload map[string]any) (ClaudeState, []state.Effect) {
	cs = absorbIdentity(cs, payload)
	now := payloadTime(payload)
	if !now.IsZero() {
		cs.StatusChangedAt = now
	}

	var effs []state.Effect

	// Watch the transcript file (if known) so future writes feed
	// DEvTranscriptChanged.
	if path := d.resolveTranscriptPath(cs); path != "" && cs.WatchedTranscript != path {
		cs.WatchedTranscript = path
		effs = append(effs, state.EffWatchTranscript{Path: path})

		// Kick off an initial parse so the title shows up before the
		// next file change.
		if !cs.TranscriptInFlight {
			cs.TranscriptInFlight = true
			effs = append(effs, state.EffStartJob{
				Kind: state.JobTranscriptParse,
				Input: TranscriptParseInput{
					ClaudeUUID: cs.ClaudeSessionID,
					Path:       path,
				},
			})
		}
	}

	// Append a SessionStart marker to the event log.
	effs = append(effs, state.EffEventLogAppend{Line: "SessionStart"})

	return cs, effs
}

// handleStateChange parses the status from the payload, advances the
// status machine, and emits an event log line.
func (d ClaudeDriver) handleStateChange(cs ClaudeState, payload map[string]any) (ClaudeState, []state.Effect) {
	cs = absorbIdentity(cs, payload)

	statusStr, _ := payload["state"].(string)
	logLine, _ := payload["log"].(string)
	now := payloadTime(payload)
	if now.IsZero() {
		now = cs.StatusChangedAt
	}

	if status, ok := state.ParseStatus(statusStr); ok {
		cs.Status = status
		cs.StatusChangedAt = now
	}

	if logLine == "" {
		logLine = statusStr
	}

	var effs []state.Effect
	if logLine != "" {
		effs = append(effs, state.EffEventLogAppend{Line: logLine})
	}

	// Trigger a transcript reparse since the new state usually means
	// new content was just written. The fsnotify watcher will also
	// fire, but kicking it here removes one tick of latency.
	if !cs.TranscriptInFlight {
		if path := d.resolveTranscriptPath(cs); path != "" {
			cs.TranscriptInFlight = true
			effs = append(effs, state.EffStartJob{
				Kind: state.JobTranscriptParse,
				Input: TranscriptParseInput{
					ClaudeUUID: cs.ClaudeSessionID,
					Path:       path,
				},
			})
		}
	}

	return cs, effs
}

// handleUserPromptSubmit captures the freshly submitted user prompt,
// updates LastPrompt without waiting for the JSONL flush, and (if
// no summary is in flight) kicks a haiku summarizer job.
func (d ClaudeDriver) handleUserPromptSubmit(cs ClaudeState, payload map[string]any) (ClaudeState, []state.Effect) {
	cs = absorbIdentity(cs, payload)
	now := payloadTime(payload)
	if !now.IsZero() {
		cs.StatusChangedAt = now
	}

	prompt, _ := payload["prompt"].(string)
	if prompt != "" {
		cs.LastPrompt = prompt
	}

	var effs []state.Effect
	effs = append(effs, state.EffEventLogAppend{Line: "UserPromptSubmit"})

	// Kick a haiku summary refresh unless one is in flight. The
	// reducer fills in JobID + SessionID; we just declare the request.
	if !cs.SummaryInFlight && prompt != "" {
		cs.SummaryInFlight = true
		effs = append(effs, state.EffStartJob{
			Kind: state.JobHaikuSummary,
			Input: HaikuSummaryInput{
				Prompt: prompt, // worker assembles the full body
			},
		})
	}

	return cs, effs
}

// absorbIdentity copies session_id / working_dir / transcript_path
// from the hook payload into the ClaudeState. Used by every hook
// handler since any of them may be the first to learn an identity.
func absorbIdentity(cs ClaudeState, payload map[string]any) ClaudeState {
	if v, ok := payload["session_id"].(string); ok && v != "" {
		cs.ClaudeSessionID = v
	}
	if v, ok := payload["working_dir"].(string); ok && v != "" {
		cs.WorkingDir = v
	}
	if v, ok := payload["transcript_path"].(string); ok && v != "" {
		cs.TranscriptPath = v
	}
	return cs
}

// payloadTime extracts an optional "now" timestamp from the payload.
// The reducer typically attaches state.Now under the key "now" so the
// driver doesn't have to read wall-clock from inside Step.
func payloadTime(payload map[string]any) time.Time {
	if v, ok := payload["now"].(time.Time); ok {
		return v
	}
	return time.Time{}
}
