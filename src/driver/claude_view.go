package driver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/takezoh/agent-roost/lib/claude/transcript"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/fishpath-go"
)

// view constructs a state.View snapshot from the cached ClaudeState.
// View building is pure: no I/O, no detection. Heavy work happens in
// Step before view is called.
//
// Card content:
//   - Title    = transcript title (set by transcript parse result)
//   - Subtitle = haiku-generated session summary, falling back to
//     LastPrompt while haiku is still computing or hasn't
//     run yet. LastPrompt is now seeded from
//     UserPromptSubmit hook payload directly so it's
//     populated even on the first turn of a brand-new
//     session before Claude has flushed anything to JSONL.
//   - Tags     = [BranchTag?]
//   - Indicators = derived from CurrentTool / SubagentCounts
//
// StatusLine: cached from the transcript parse result.
func (d ClaudeDriver) view(cs ClaudeState) state.View {
	var tags []state.Tag
	if t := BranchTag(cs.BranchTag, cs.BranchBG, cs.BranchFG, cs.BranchParentBranch); t.Text != "" {
		tags = append(tags, t)
	}

	var logTabs []state.LogTab
	if transcriptPath := d.resolveTranscriptPath(cs); transcriptPath != "" {
		rendererCfg, _ := json.Marshal(transcript.RendererConfig{
			SubagentDir:  subagentDir(transcriptPath),
			ShowThinking: d.showThinking,
		})
		logTabs = append(logTabs, state.LogTab{
			Label:       "TRANSCRIPT",
			Path:        transcriptPath,
			Kind:        transcript.KindTranscript,
			RendererCfg: rendererCfg,
		})
	}
	if cs.RoostSessionID != "" && d.eventLogDir != "" {
		logTabs = append(logTabs, state.LogTab{
			Label: "EVENTS",
			Path:  filepath.Join(d.eventLogDir, cs.RoostSessionID+".log"),
			Kind:  state.TabKindText,
		})
	}

	return state.View{
		Card: state.Card{
			Title:       cs.Title,
			Subtitle:    firstNonEmpty(cs.Summary, cs.LastPrompt),
			Tags:        tags,
			Indicators:  claudeIndicators(cs),
			BorderTitle: CommandTag(ClaudeDriverName),
			BorderBadge: fishpath.Shorten(cs.WorkingDir, d.home),
		},
		DisplayName:     ClaudeDriverName,
		LogTabs:         logTabs,
		InfoExtras:      claudeInfoExtras(cs),
		StatusLine:      cs.StatusLine,
		Status:          cs.Status,
		StatusChangedAt: cs.StatusChangedAt,
	}
}

func claudeIndicators(cs ClaudeState) []string {
	var out []string
	if cs.HangDetected {
		out = append(out, "stale?")
	}
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
	if cs.BranchIsWorktree {
		add("Parent Branch", cs.BranchParentBranch)
	}
	add("Transcript", cs.TranscriptPath)
	return lines
}

func subagentDir(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	if !strings.HasSuffix(transcriptPath, ".jsonl") {
		return ""
	}
	base := strings.TrimSuffix(transcriptPath, ".jsonl")
	return base + string(os.PathSeparator) + "subagents"
}

func firstNonEmpty(candidates ...string) string {
	for _, s := range candidates {
		if s != "" {
			return s
		}
	}
	return ""
}
