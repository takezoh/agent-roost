package runtime

import (
	"log/slog"

	"github.com/take/agent-roost/proto"
	"github.com/take/agent-roost/state"
)

// Bridge functions that translate state-package payloads (which are
// kept proto-agnostic) into proto Response / ServerEvent values, and
// queue them on the right connection's outbox.
//
// These are called from interpret.go when it sees an IPC effect.

// sendResponse encodes a typed Response from the state-side body and
// queues it on the matching connection. Body shapes:
//   - nil                      → proto.RespOK
//   - state.CreateSessionReply → proto.RespCreateSession
//   - state.SessionsReply      → proto.RespSessions (materialized from current state)
//   - state.ActiveWindowReply  → proto.RespActiveWindow
func (r *Runtime) sendResponse(e state.EffSendResponse) {
	cc, ok := r.conns[e.ConnID]
	if !ok {
		return
	}
	resp := r.translateResponseBody(e.Body)
	wire, err := proto.EncodeResponse(e.ReqID, resp)
	if err != nil {
		slog.Error("runtime: encode response failed", "err", err)
		return
	}
	r.queueWire(cc, wire)
}

// translateResponseBody maps a state-package body to its proto
// counterpart. The state pkg returns a placeholder marker for the
// sessions list (state.SessionsReply{}) — runtime fills it in here
// because only runtime can build proto.SessionInfo (the materializer
// needs driver.View() which is in state, but the SessionInfo struct
// is in proto).
func (r *Runtime) translateResponseBody(body any) proto.Response {
	switch b := body.(type) {
	case nil:
		return proto.RespOK{}
	case state.CreateSessionReply:
		return proto.RespCreateSession{
			SessionID: b.SessionID,
			WindowID:  b.WindowID,
		}
	case state.SessionsReply:
		infos, active := r.buildSessionInfos()
		return proto.RespSessions{Sessions: infos, ActiveWindowID: active}
	case state.ActiveWindowReply:
		return proto.RespActiveWindow{ActiveWindowID: b.ActiveWindowID}
	}
	slog.Warn("runtime: unknown response body type, sending RespOK",
		"type", typeNameOf(body))
	return proto.RespOK{}
}

func (r *Runtime) sendError(e state.EffSendError) {
	cc, ok := r.conns[e.ConnID]
	if !ok {
		return
	}
	wire, err := proto.EncodeError(e.ReqID, proto.FromStateCode(e.Code), e.Message, e.Details)
	if err != nil {
		slog.Error("runtime: encode error failed", "err", err)
		return
	}
	r.queueWire(cc, wire)
}

// broadcastSessionsChanged builds the sessions-changed event from
// current state and queues it on every subscribed connection.
func (r *Runtime) broadcastSessionsChanged(preview bool) {
	infos, active := r.buildSessionInfos()
	ev := proto.EvtSessionsChanged{
		Sessions:       infos,
		ActiveWindowID: active,
		IsPreview:      preview,
	}
	wire, err := proto.EncodeEvent(ev)
	if err != nil {
		slog.Error("runtime: encode sessions-changed failed", "err", err)
		return
	}
	r.broadcastWire(wire, proto.EvtNameSessionsChanged)
}

// broadcastGenericEvent translates a state.EffBroadcastEvent into the
// matching proto.ServerEvent and broadcasts it. The Name field on
// the effect determines the variant.
func (r *Runtime) broadcastGenericEvent(e state.EffBroadcastEvent) {
	var event proto.ServerEvent
	switch e.Name {
	case "project-selected":
		if p, ok := e.Payload.(state.ProjectSelectedPayload); ok {
			event = proto.EvtProjectSelected{Project: p.Project}
		}
	case "pane-focused":
		if p, ok := e.Payload.(state.PaneFocusedPayload); ok {
			event = proto.EvtPaneFocused{Pane: p.Pane}
		}
	}
	if event == nil {
		slog.Warn("runtime: unknown broadcast event", "name", e.Name)
		return
	}
	wire, err := proto.EncodeEvent(event)
	if err != nil {
		slog.Error("runtime: encode broadcast failed", "err", err)
		return
	}
	r.broadcastWire(wire, e.Name)
}

// broadcastWire fan-outs raw wire bytes to every subscribed
// connection. Filter is the event name; subscribers with non-empty
// Filters lists must include this name to receive the message.
func (r *Runtime) broadcastWire(wire []byte, eventName string) {
	for connID, sub := range r.state.Subscribers {
		if !subscriptionMatches(sub, eventName) {
			continue
		}
		cc, ok := r.conns[connID]
		if !ok {
			continue
		}
		// Outbox sends are non-blocking via queueWire — slow clients
		// drop, fast ones receive.
		select {
		case cc.outbox <- wire:
		case <-cc.done:
		default:
			slog.Warn("runtime: subscriber outbox full, dropping",
				"conn", connID, "event", eventName)
		}
	}
}

// subscriptionMatches reports whether a Subscriber wants to receive
// an event with the given name. Empty Filters list = all events.
func subscriptionMatches(sub state.Subscriber, eventName string) bool {
	if len(sub.Filters) == 0 {
		return true
	}
	for _, f := range sub.Filters {
		if f == eventName {
			return true
		}
	}
	return false
}

func (r *Runtime) closeConn(id state.ConnID) {
	if cc, ok := r.conns[id]; ok {
		cc.shut()
		delete(r.conns, id)
	}
}

// buildSessionInfos materializes the current state.Sessions map into
// the proto.SessionInfo wire format. Calls each driver's View() pure
// getter to fill the View payload.
func (r *Runtime) buildSessionInfos() ([]proto.SessionInfo, string) {
	infos := make([]proto.SessionInfo, 0, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		drv := state.GetDriver(sess.Command)
		var view state.View
		if drv != nil {
			view = drv.View(sess.Driver)
		}
		// Append the EVENTS log tab the runtime owns (driver only
		// declares its intent — the runtime knows the file path).
		view = r.appendEventLogTab(view, sess.ID)

		info := proto.SessionInfo{
			ID:          string(sess.ID),
			Project:     sess.Project,
			Command:     sess.Command,
			WindowID:    string(sess.WindowID),
			AgentPaneID: sess.AgentPaneID,
			CreatedAt:   sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			State:       view.Status,
			View:        view,
		}
		if !view.StatusChangedAt.IsZero() {
			info.StateChangedAt = view.StatusChangedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		infos = append(infos, info)
	}
	return infos, string(r.state.Active)
}

// appendEventLogTab attaches the EVENTS LogTab to a view, using the
// runtime's eventlog backend to resolve the on-disk path. Drivers
// don't know the eventLog directory, so the runtime stitches it in
// when serializing.
func (r *Runtime) appendEventLogTab(view state.View, sessionID state.SessionID) state.View {
	if fl, ok := r.cfg.EventLog.(*FileEventLog); ok {
		view.LogTabs = append(view.LogTabs, state.LogTab{
			Label: "EVENTS",
			Path:  fl.Path(sessionID),
			Kind:  state.TabKindText,
		})
	}
	return view
}

func typeNameOf(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case state.CreateSessionReply:
		return "state.CreateSessionReply"
	case state.SessionsReply:
		return "state.SessionsReply"
	case state.ActiveWindowReply:
		return "state.ActiveWindowReply"
	}
	return "unknown"
}
