package runtime

import (
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func commandToStateEvent(connID state.ConnID, reqID string, cmd proto.Command) state.Event {
	switch c := cmd.(type) {
	case proto.CmdSubscribe:
		return state.EvCmdSubscribe{ConnID: connID, ReqID: reqID, Filters: c.Filters}
	case proto.CmdUnsubscribe:
		return state.EvCmdUnsubscribe{ConnID: connID, ReqID: reqID}
	case proto.CmdEvent:
		if state.IsRegisteredEvent(c.Event) {
			return state.EvEvent{
				ConnID:  connID,
				ReqID:   reqID,
				Event:   c.Event,
				Payload: c.Payload,
			}
		}
		return state.EvDriverEvent{
			ConnID:    connID,
			ReqID:     reqID,
			Event:     c.Event,
			Timestamp: c.Timestamp,
			SenderID:  state.FrameID(c.SenderID),
			Payload:   c.Payload,
		}
	case proto.CmdSurfaceReadText:
		return state.EvCmdSurfaceReadText{
			ConnID:    connID,
			ReqID:     reqID,
			SessionID: state.SessionID(c.SessionID),
			Lines:     c.Lines,
		}
	case proto.CmdSurfaceSendText:
		return state.EvCmdSurfaceSendText{
			ConnID:    connID,
			ReqID:     reqID,
			SessionID: state.SessionID(c.SessionID),
			Text:      c.Text,
		}
	case proto.CmdSurfaceSendKey:
		return state.EvCmdSurfaceSendKey{
			ConnID:    connID,
			ReqID:     reqID,
			SessionID: state.SessionID(c.SessionID),
			Key:       c.Key,
		}
	case proto.CmdDriverList:
		return state.EvCmdDriverList{ConnID: connID, ReqID: reqID}
	}
	return nil
}
