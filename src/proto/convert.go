package proto

import (
	"github.com/take/agent-roost/state"
)

// CommandToStateEvent translates an incoming typed Command into the
// state.Event the reducer dispatches on. ConnID and ReqID come from
// the connection that received the command (the runtime tracks them).
//
// Returns nil for unknown commands so the runtime can fall through
// to a generic "unknown command" error response.
func CommandToStateEvent(connID state.ConnID, reqID string, cmd Command) state.Event {
	switch c := cmd.(type) {
	case CmdSubscribe:
		return state.EvCmdSubscribe{ConnID: connID, ReqID: reqID, Filters: c.Filters}
	case CmdUnsubscribe:
		return state.EvCmdUnsubscribe{ConnID: connID, ReqID: reqID}
	case CmdCreateSession:
		return state.EvCmdCreateSession{ConnID: connID, ReqID: reqID, Project: c.Project, Command: c.Command}
	case CmdStopSession:
		return state.EvCmdStopSession{ConnID: connID, ReqID: reqID, SessionID: state.SessionID(c.SessionID)}
	case CmdListSessions:
		return state.EvCmdListSessions{ConnID: connID, ReqID: reqID}
	case CmdPreviewSession:
		return state.EvCmdPreviewSession{ConnID: connID, ReqID: reqID, SessionID: state.SessionID(c.SessionID)}
	case CmdSwitchSession:
		return state.EvCmdSwitchSession{ConnID: connID, ReqID: reqID, SessionID: state.SessionID(c.SessionID)}
	case CmdPreviewProject:
		return state.EvCmdPreviewProject{ConnID: connID, ReqID: reqID, Project: c.Project}
	case CmdFocusPane:
		return state.EvCmdFocusPane{ConnID: connID, ReqID: reqID, Pane: c.Pane}
	case CmdLaunchTool:
		return state.EvCmdLaunchTool{ConnID: connID, ReqID: reqID, Tool: c.Tool, Args: c.Args}
	case CmdHook:
		return state.EvCmdHook{
			ConnID:    connID,
			ReqID:     reqID,
			Driver:    c.Driver,
			Event:     c.Event,
			SessionID: state.SessionID(c.SessionID),
			Payload:   c.Payload,
		}
	case CmdShutdown:
		return state.EvCmdShutdown{ConnID: connID, ReqID: reqID}
	case CmdDetach:
		return state.EvCmdDetach{ConnID: connID, ReqID: reqID}
	}
	return nil
}
