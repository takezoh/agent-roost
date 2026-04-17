package driver

import "github.com/takezoh/agent-roost/state"

func (d ShellDriver) view(ss ShellState) state.View {
	return state.View{
		Card: state.Card{
			Subtitle:    ss.Summary,
			BorderTitle: ShellCommandTag(d.displayName),
			Tags:        CommonTags(ss.CommonState),
		},
		DisplayName:     d.displayName,
		Status:          ss.Status,
		StatusChangedAt: ss.StatusChangedAt,
	}
}
