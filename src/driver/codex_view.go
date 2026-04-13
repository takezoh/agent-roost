package driver

import (
	"encoding/json"

	codextranscript "github.com/takezoh/agent-roost/lib/codex/transcript"
	"github.com/takezoh/agent-roost/state"
	"github.com/takezoh/fishpath-go"
)

func (d CodexDriver) view(cs CodexState) state.View {
	tags := CommonTags(cs.CommonState)

	var tabs []state.LogTab
	if cs.TranscriptPath != "" {
		cfg, _ := json.Marshal(codextranscript.RendererConfig{})
		tabs = append(tabs, state.LogTab{
			Label:       "TRANSCRIPT",
			Path:        cs.TranscriptPath,
			Kind:        codextranscript.KindTranscript,
			RendererCfg: cfg,
		})
	}
	if tab := EventLogTab(cs.CommonState, d.eventLogDir); tab != nil {
		tabs = append(tabs, *tab)
	}

	return state.View{
		Card: state.Card{
			Title:       cs.Title,
			Subtitle:    firstNonEmpty(cs.LastPrompt, cs.LastAssistantMessage),
			Tags:        tags,
			BorderTitle: CodexCommandTag(),
			BorderBadge: fishpath.Shorten(cs.WorkingDir, ""),
		},
		DisplayName:     CodexDriverName,
		LogTabs:         tabs,
		InfoExtras:      codexInfoExtras(cs),
		StatusLine:      cs.StatusLine,
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
	add("Title", cs.Title)
	add("Codex Session", cs.CodexSessionID)
	add("Working Dir", cs.WorkingDir)
	add("Managed Worktree", cs.ManagedWorkingDir)
	add("Worktree Name", cs.WorktreeName)
	if cs.BranchIsWorktree {
		add("Parent Branch", cs.BranchParentBranch)
	}
	add("Transcript", cs.TranscriptPath)
	add("Status Line", cs.StatusLine)
	add("Last Prompt", cs.LastPrompt)
	add("Last Assistant", cs.LastAssistantMessage)
	add("Last Hook", cs.LastHookEvent)
	return lines
}
