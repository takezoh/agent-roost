package driver

import (
	"path/filepath"

	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/fishpath-go"
)

func (d GeminiDriver) view(gs GeminiState) state.View {
	var tags []state.Tag
	if t := BranchTag(gs.BranchTag, gs.BranchBG, gs.BranchFG, gs.BranchParentBranch); t.Text != "" {
		tags = append(tags, t)
	}

	var tabs []state.LogTab
	if gs.RoostSessionID != "" && d.eventLogDir != "" {
		tabs = append(tabs, state.LogTab{
			Label: "EVENTS",
			Path:  filepath.Join(d.eventLogDir, gs.RoostSessionID+".log"),
			Kind:  state.TabKindText,
		})
	}

	return state.View{
		Card: state.Card{
			Subtitle:    firstNonEmpty(gs.LastPrompt, gs.LastAssistantMessage),
			Tags:        tags,
			BorderTitle: GeminiCommandTag(),
			BorderBadge: fishpath.Shorten(gs.WorkingDir, ""),
		},
		DisplayName:     GeminiDriverName,
		LogTabs:         tabs,
		InfoExtras:      geminiInfoExtras(gs),
		Status:          gs.Status,
		StatusChangedAt: gs.StatusChangedAt,
	}
}

func geminiInfoExtras(gs GeminiState) []state.InfoLine {
	var lines []state.InfoLine
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, state.InfoLine{Label: label, Value: value})
		}
	}
	add("Gemini Session", gs.GeminiSessionID)
	add("Working Dir", gs.WorkingDir)
	if gs.ManagedWorkingDir != "" {
		add("Managed Worktree", gs.ManagedWorkingDir)
		add("Worktree Name", gs.WorktreeName)
	}
	if gs.BranchIsWorktree {
		add("Parent Branch", gs.BranchParentBranch)
	}
	add("Transcript", gs.TranscriptPath)
	add("Last Prompt", previewText(gs.LastPrompt))
	add("Last Assistant", previewText(gs.LastAssistantMessage))
	add("Last Hook", gs.LastHookEvent)
	return lines
}
