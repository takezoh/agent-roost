package driver

import (
	"path/filepath"

	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/fishpath-go"
)

func (d CodexDriver) view(cs CodexState) state.View {
	var tags []state.Tag
	if t := BranchTag(cs.BranchTag, cs.BranchBG, cs.BranchFG, cs.BranchParentBranch); t.Text != "" {
		tags = append(tags, t)
	}

	var tabs []state.LogTab
	if cs.RoostSessionID != "" && d.eventLogDir != "" {
		tabs = append(tabs, state.LogTab{
			Label: "EVENTS",
			Path:  filepath.Join(d.eventLogDir, cs.RoostSessionID+".log"),
			Kind:  state.TabKindText,
		})
	}

	return state.View{
		Card: state.Card{
			Subtitle:    firstNonEmpty(cs.LastPrompt, cs.LastAssistantMessage),
			Tags:        tags,
			BorderTitle: CodexCommandTag(),
			BorderBadge: fishpath.Shorten(cs.WorkingDir, ""),
		},
		DisplayName:     CodexDriverName,
		LogTabs:         tabs,
		InfoExtras:      codexInfoExtras(cs),
		Status:          cs.Status,
		StatusChangedAt: cs.StatusChangedAt,
	}
}

func codexInfoExtras(cs CodexState) []state.InfoLine {
	var lines []state.InfoLine
	add := func(label, value string) {
		if value != "" {
			lines = append(lines, state.InfoLine{Label: label, Value: value})
		}
	}
	add("Codex Session", cs.CodexSessionID)
	add("Working Dir", cs.WorkingDir)
	if cs.BranchIsWorktree {
		add("Parent Branch", cs.BranchParentBranch)
	}
	add("Transcript", cs.TranscriptPath)
	add("Last Prompt", cs.LastPrompt)
	add("Last Assistant", cs.LastAssistantMessage)
	add("Last Hook", cs.LastHookEvent)
	return lines
}
