package driver

import (
	"fmt"

	"github.com/takezoh/agent-roost/state"
)

func (d ShellDriver) view(ss ShellState) state.View {
	return state.View{
		Card: state.Card{
			Subtitle:    ss.Summary,
			BorderTitle: ShellCommandTag(d.displayName),
			Tags:        shellTags(ss),
		},
		DisplayName:     d.displayName,
		Status:          ss.Status,
		StatusChangedAt: ss.StatusChangedAt,
	}
}

func shellTags(ss ShellState) []state.Tag {
	tags := CommonTags(ss.CommonState)
	if ss.LastExitCode != nil && *ss.LastExitCode != 0 {
		tags = append(tags, state.Tag{
			Text:       fmt.Sprintf("✘ %d", *ss.LastExitCode),
			Foreground: "#ffffff",
			Background: "#cc3333",
		})
	}
	return tags
}
