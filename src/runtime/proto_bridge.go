package runtime

import (
	"log/slog"
	"sort"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
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
	wire, err := r.encodeResponse(e.ReqID, e.Body)
	if err != nil {
		slog.Error("runtime: encode response failed", "err", err)
		return
	}
	r.queueWire(cc, wire)
}

func (r *Runtime) sendResponseSync(e state.EffSendResponseSync) {
	cc, ok := r.conns[e.ConnID]
	if !ok {
		return
	}
	wire, err := r.encodeResponse(e.ReqID, e.Body)
	if err != nil {
		slog.Error("runtime: encode sync response failed", "err", err)
		return
	}
	if err := r.writeWire(cc, wire); err != nil {
		slog.Debug("runtime: sync response write failed", "conn", e.ConnID, "err", err)
	}
}

func (r *Runtime) encodeResponse(reqID string, body any) ([]byte, error) {
	resp := r.translateResponseBody(body)
	return proto.EncodeResponse(reqID, resp)
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
		}
	case state.SessionsReply:
		infos, active := r.buildSessionInfos()
		return proto.RespSessions{Sessions: infos, ActiveSessionID: active, Connectors: r.buildConnectorInfos(), Features: r.buildFeatureList()}
	case state.ActiveSessionReply:
		return proto.RespActiveSession{ActiveSessionID: b.ActiveSessionID}
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
		Sessions:        infos,
		ActiveSessionID: active,
		IsPreview:       preview,
		Connectors:      r.buildConnectorInfos(),
		Features:        r.buildFeatureList(),
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
	// Sort by CreatedAt for stable card ordering — Go map iteration
	// is randomized, so without sorting the TUI cards would shuffle
	// on every broadcast.
	sorted := make([]state.Session, 0, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		sorted = append(sorted, sess)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	infos := make([]proto.SessionInfo, 0, len(sorted))
	for _, sess := range sorted {
		frame, ok := sessionRootFrame(sess)
		if !ok {
			continue
		}
		drv := state.GetDriver(frame.Command)
		var view state.View
		if drv != nil {
			view = drv.View(frame.Driver)
		}
		// When the frame stack has more than one frame, show the active
		// frame's BorderTitle as a secondary chip on the card border.
		if len(sess.Frames) > 1 {
			if activeF, ok := sessionActiveFrame(sess); ok {
				activeDrv := state.GetDriver(activeF.Command)
				if activeDrv != nil {
					activeView := activeDrv.View(activeF.Driver)
					view.Card.BorderTitleSecondary = activeView.Card.BorderTitle
				}
			}
		}
		info := proto.SessionInfo{
			ID:        string(sess.ID),
			Project:   sess.Project,
			Workspace: r.workspaceResolver.Resolve(sess.Project),
			Command:   frame.Command,
			CreatedAt: sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			State:     view.Status,
			View:      view,
		}
		if !view.StatusChangedAt.IsZero() {
			info.StateChangedAt = view.StatusChangedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		infos = append(infos, info)
	}
	return infos, string(r.activeSession)
}

// buildConnectorInfos materializes the current connector states into
// the proto.ConnectorInfo wire format. Calls each connector's View()
// pure getter to fill the payload. Only includes available connectors.
func (r *Runtime) buildConnectorInfos() []proto.ConnectorInfo {
	connectors := state.AllConnectors()
	if len(connectors) == 0 {
		return nil
	}
	var infos []proto.ConnectorInfo
	for _, c := range connectors {
		cs := r.state.Connectors[c.Name()]
		if cs == nil {
			continue
		}
		view := c.View(cs)
		if !view.Available {
			continue
		}
		infos = append(infos, proto.ConnectorInfo{
			Name:      c.Name(),
			Label:     view.Label,
			Summary:   view.Summary,
			Available: view.Available,
			Sections:  view.Sections,
		})
	}
	return infos
}

// buildFeatureList converts the state's runtime feature set into the wire
// format ([]string of enabled flag names). Returns nil when no flags are
// enabled so the JSON field is omitted.
func (r *Runtime) buildFeatureList() []string {
	if len(r.state.Features) == 0 {
		return nil
	}
	var list []string
	for f, on := range r.state.Features {
		if on {
			list = append(list, string(f))
		}
	}
	return list
}

func typeNameOf(v any) string {
	switch v.(type) {
	case nil:
		return "nil"
	case state.CreateSessionReply:
		return "state.CreateSessionReply"
	case state.SessionsReply:
		return "state.SessionsReply"
	case state.ActiveSessionReply:
		return "state.ActiveSessionReply"
	}
	return "unknown"
}
