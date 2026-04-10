package driver

import (
	"fmt"

	"github.com/take/agent-roost/state"
)

// view constructs a state.View snapshot from the cached ClaudeState.
// View building is pure: no I/O, no detection. Heavy work happens in
// Step before view is called.
//
// Card content:
//   - Title    = transcript title (set by transcript parse result)
//   - Subtitle = haiku-generated session summary, falling back to
//                LastPrompt while haiku is still computing or hasn't
//                run yet. LastPrompt is now seeded from
//                UserPromptSubmit hook payload directly so it's
//                populated even on the first turn of a brand-new
//                session before Claude has flushed anything to JSONL.
//   - Tags     = [CommandTag("claude"), BranchTag(BranchTag?)]
//   - Indicators = derived from CurrentTool / SubagentCounts
//
// LogTabs are emitted by the runtime since the runtime is the only
// component that knows the eventLogDir + per-session paths. The
// driver only emits the abstract intent (transcript path) and the
// runtime materializes the LogTab list when building proto payloads.
//
// StatusLine: cached from the transcript parse result.
func (d ClaudeDriver) view(cs ClaudeState) state.View {
	tags := []state.Tag{CommandTag("claude")}
	if t := BranchTag(cs.BranchTag, cs.BranchVCS); t.Text != "" {
		tags = append(tags, t)
	}

	var logTabs []state.LogTab
	if transcriptPath := d.resolveTranscriptPath(cs); transcriptPath != "" {
		logTabs = append(logTabs, state.LogTab{
			Label: "TRANSCRIPT",
			Path:  transcriptPath,
			Kind:  state.TabKindTranscript,
		})
	}
	// EVENTS tab path is filled in by the runtime when serializing
	// SessionInfo since only the runtime knows the eventLogDir base.
	// The driver declares its intent via SuppressInfo=false +
	// LogTabs not containing EVENTS; the runtime appends EVENTS in
	// proto building.

	return state.View{
		Card: state.Card{
			Title:      cs.Title,
			Subtitle:   firstNonEmpty(cs.Summary, cs.LastPrompt),
			Tags:       tags,
			Indicators: claudeIndicators(cs),
		},
		LogTabs:         logTabs,
		InfoExtras:      claudeInfoExtras(cs),
		StatusLine:      cs.StatusLine,
		Status:          cs.Status,
		StatusChangedAt: cs.StatusChangedAt,
	}
}

func claudeIndicators(cs ClaudeState) []string {
	var out []string
	if cs.CurrentTool != "" {
		out = append(out, "▸ "+cs.CurrentTool)
	}
	subs := 0
	for _, n := range cs.SubagentCounts {
		subs += n
	}
	if subs > 0 {
		out = append(out, fmt.Sprintf("%d subs", subs))
	}
	return out
}

func claudeInfoExtras(cs ClaudeState) []state.InfoLine {
	var lines []state.InfoLine
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, state.InfoLine{Label: label, Value: value})
		}
	}
	add("Title", cs.Title)
	add("Summary", cs.Summary)
	add("Last Prompt", cs.LastPrompt)
	add("Working Dir", cs.WorkingDir)
	add("Transcript", cs.TranscriptPath)
	return lines
}

func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}
