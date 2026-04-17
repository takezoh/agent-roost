package driver

import "github.com/takezoh/agent-roost/state"

// view returns the minimal View for a generic session. DisplayName and
// BorderTitle are driven solely by d.displayName — for the built-in
// fallback driver (displayName == "") no command tag is rendered.
func (d GenericDriver) view(gs GenericState) state.View {
	var borderTitle state.Tag
	if d.displayName != "" {
		borderTitle = CommandTag(d.displayName)
	}
	return state.View{
		Card: state.Card{
			Subtitle:    gs.Summary,
			BorderTitle: borderTitle,
			Tags:        CommonTags(gs.CommonState),
		},
		DisplayName:     d.displayName,
		Status:          gs.Status,
		StatusChangedAt: gs.StatusChangedAt,
	}
}
