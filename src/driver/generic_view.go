package driver

import "github.com/take/agent-roost/state"

// view returns the minimal View for a generic (non-Claude) session.
// Driver-specific UI elements are DisplayName and BorderTitle.
// Everything else (state symbol, generic INFO header, project name,
// elapsed time) is rendered by the TUI from proto.SessionInfo.
func (d GenericDriver) view(gs GenericState) state.View {
	var borderTitle state.Tag
	if d.displayName != "" {
		borderTitle = CommandTag(d.displayName)
	}
	return state.View{
		Card:            state.Card{BorderTitle: borderTitle},
		DisplayName:     d.displayName,
		Status:          gs.Status,
		StatusChangedAt: gs.StatusChangedAt,
	}
}
