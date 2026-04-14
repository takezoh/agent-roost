package driver

import (
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/fishpath-go"
)

func (d GeminiDriver) view(gs GeminiState) state.View {
	tags := CommonTags(gs.CommonState)

	var tabs []state.LogTab
	if tab := EventLogTab(gs.CommonState, d.eventLogDir); tab != nil {
		tabs = append(tabs, *tab)
	}

	return state.View{
		Card: state.Card{
			Subtitle:    firstNonEmpty(gs.LastPrompt, gs.LastAssistantMessage),
			Tags:        tags,
			BorderTitle: GeminiCommandTag(),
			BorderBadge: fishpath.Shorten(gs.StartDir, ""),
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
	add("Working Dir", gs.StartDir)
	if gs.BranchIsWorktree {
		add("Parent Branch", gs.BranchParentBranch)
	}
	add("Transcript", gs.TranscriptPath)
	add("Last Prompt", previewText(gs.LastPrompt))
	add("Last Assistant", previewText(gs.LastAssistantMessage))
	add("Last Hook", gs.LastHookEvent)
	return lines
}
