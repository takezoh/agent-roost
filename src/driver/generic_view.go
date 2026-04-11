package driver

import "github.com/take/agent-roost/state"

// view returns the minimal View for a generic (non-Claude) session.
// The only driver-specific UI element is the command tag — everything
// else (state symbol, generic INFO header, project name, elapsed time)
// is rendered by the TUI from proto.SessionInfo. Drivers with no
// display name (the unnamed fallback driver) emit no command tag
// rather than an empty colored chip.
func (d GenericDriver) view(gs GenericState) state.View {
	var tags []state.Tag
	if d.displayName != "" {
		tags = []state.Tag{CommandTag(d.displayName)}
	}
	return state.View{
		Card:            state.Card{Tags: tags, BorderTitle: d.displayName},
		Status:          gs.Status,
		StatusChangedAt: gs.StatusChangedAt,
	}
}
