package runtime

import (
	"fmt"
	"log/slog"
	"sort"
	"time"

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
		infos, active, occupant := r.buildSessionInfos()
		return proto.RespSessions{Sessions: infos, ActiveSessionID: active, ActiveOccupant: occupant, Connectors: r.buildConnectorInfos(), Features: r.buildFeatureList()}
	case state.ActiveSessionReply:
		return proto.RespActiveSession{ActiveSessionID: b.ActiveSessionID}
	case state.SurfaceReadTextReply:
		return r.buildSurfaceText(b)
	case state.DriverListReply:
		return r.buildDriverList()
	case state.PeerListReply:
		return r.buildPeerListResp(b)
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

// syncRelayWatches ensures every LogTab path for every active session is
// registered with the FileRelay. Called before each sessions-changed
// broadcast so that sessions created at runtime get their push channels
// wired up without requiring drivers to emit EffWatchFile for non-transcript
// tabs (e.g. the EVENTS tab with TabKindText).
//
// FileRelay.add is idempotent by path, so repeated calls are safe.
func (r *Runtime) syncRelayWatches() {
	if r.relay == nil {
		return
	}
	for _, sess := range r.state.Sessions {
		frame, ok := sessionRootFrame(sess)
		if !ok {
			continue
		}
		drv := state.GetDriver(frame.Command)
		if drv == nil {
			continue
		}
		for _, lt := range drv.View(frame.Driver).LogTabs {
			if lt.Path != "" {
				r.relay.WatchFile(frame.ID, lt.Path, string(lt.Kind))
			}
		}
	}
}

// broadcastSessionsChanged builds the sessions-changed event from
// current state and queues it on every subscribed connection.
func (r *Runtime) broadcastSessionsChanged(preview bool) {
	r.syncRelayWatches()
	infos, active, occupant := r.buildSessionInfos()
	ev := proto.EvtSessionsChanged{
		Sessions:        infos,
		ActiveSessionID: active,
		ActiveOccupant:  occupant,
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
	case "peer-message":
		if p, ok := e.Payload.(state.PeerMessagePayload); ok {
			event = proto.EvtPeerMessage{
				ToSessionID: string(p.ToSessionID),
				FromFrameID: string(p.FromFrameID),
				Text:        p.Text,
				SentAt:      p.SentAt.Format(time.RFC3339),
			}
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

// broadcastAgentNotification encodes and broadcasts an OSC notification
// captured from an agent pane.
func (r *Runtime) broadcastAgentNotification(e state.EffRecordNotification) {
	ev := proto.EvtAgentNotification{
		SessionID: string(e.SessionID),
		Cmd:       e.Cmd,
		Title:     e.Title,
		Body:      e.Body,
	}
	wire, err := proto.EncodeEvent(ev)
	if err != nil {
		slog.Error("runtime: encode agent notification failed", "err", err)
		return
	}
	r.broadcastWire(wire, proto.EvtNameAgentNotification)
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
// getter to fill the View payload. Returns sessions, active session id,
// and active occupant kind string ("main" | "log" | "frame").
func (r *Runtime) buildSessionInfos() ([]proto.SessionInfo, string, string) {
	sorted := make([]state.Session, 0, len(r.state.Sessions))
	for _, sess := range r.state.Sessions {
		sorted = append(sorted, sess)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	infos := make([]proto.SessionInfo, 0, len(sorted))
	for _, sess := range sorted {
		if info, ok := r.buildOneSessionInfo(sess); ok {
			infos = append(infos, info)
		}
	}
	return infos, string(r.state.ActiveSession), occupantString(r.state.ActiveOccupant)
}

func occupantString(k state.OccupantKind) string {
	switch k {
	case state.OccupantLog:
		return proto.OccupantLog
	case state.OccupantFrame:
		return proto.OccupantFrame
	default:
		return proto.OccupantMain
	}
}

func (r *Runtime) buildOneSessionInfo(sess state.Session) (proto.SessionInfo, bool) {
	frame, ok := sessionRootFrame(sess)
	if !ok {
		return proto.SessionInfo{}, false
	}
	drv := state.GetDriver(frame.Command)
	var view state.View
	if drv != nil {
		view = drv.View(frame.Driver)
	}
	if len(sess.Frames) > 1 {
		if activeF, ok := sessionActiveFrame(sess); ok {
			if activeDrv := state.GetDriver(activeF.Command); activeDrv != nil {
				view.Card.BorderTitleSecondary = activeDrv.View(activeF.Driver).Card.BorderTitle
			}
		}
	}
	if len(frame.PeerInbox) > 0 && view.Card.BorderBadge == "" {
		view.Card.BorderBadge = fmt.Sprintf("💬 %d", len(frame.PeerInbox))
	}
	frames := make([]proto.FrameInfo, 0, len(sess.Frames))
	for _, sf := range sess.Frames {
		frames = append(frames, proto.FrameInfo{ID: string(sf.ID), Command: sf.Command})
	}
	activeF, _ := sessionActiveFrame(sess)
	info := proto.SessionInfo{
		ID:            string(sess.ID),
		Project:       sess.Project,
		Workspace:     r.workspaceResolver.Resolve(sess.Project),
		Command:       frame.Command,
		CreatedAt:     sess.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		State:         view.Status,
		View:          view,
		Frames:        frames,
		ActiveFrameID: string(activeF.ID),
		IsActive:      r.state.ActiveSession == sess.ID,
	}
	if !view.StatusChangedAt.IsZero() {
		info.StateChangedAt = view.StatusChangedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return info, true
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

// buildSurfaceText calls CapturePane against the session's pane and returns
// the result as a RespSurfaceText.
func (r *Runtime) buildSurfaceText(b state.SurfaceReadTextReply) proto.Response {
	pane := r.sessionPaneForSession(b.SessionID)
	if pane == "" {
		return proto.RespSurfaceText{}
	}
	text, err := r.cfg.Tmux.CapturePane(pane, b.Lines)
	if err != nil {
		slog.Warn("runtime: surface.read_text capture failed", "session", b.SessionID, "err", err)
		return proto.RespSurfaceText{}
	}
	return proto.RespSurfaceText{Text: text}
}

// buildPeerListResp converts a state.PeerListReply into the proto wire format.
func (r *Runtime) buildPeerListResp(reply state.PeerListReply) proto.RespPeerList {
	peers := make([]proto.PeerPeerInfo, 0, len(reply.Peers))
	for _, p := range reply.Peers {
		peers = append(peers, proto.PeerPeerInfo{
			FrameID:    p.FrameID,
			SessionID:  p.SessionID,
			Driver:     p.Driver,
			Project:    p.Project,
			Workspace:  r.workspaceResolver.Resolve(p.Project),
			Summary:    p.Summary,
			Status:     p.Status.String(),
			InboxCount: p.InboxCount,
		})
	}
	return proto.RespPeerList{Peers: peers}
}

// buildDriverList returns the list of registered drivers sorted by name.
func (r *Runtime) buildDriverList() proto.Response {
	reg := state.GetRegistry()
	infos := make([]proto.DriverInfo, 0, len(reg))
	for _, d := range reg {
		infos = append(infos, proto.DriverInfo{
			Name:        d.Name(),
			DisplayName: d.DisplayName(),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return proto.RespDriverList{Drivers: infos}
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
	case state.SurfaceReadTextReply:
		return "state.SurfaceReadTextReply"
	case state.DriverListReply:
		return "state.DriverListReply"
	case state.PeerListReply:
		return "state.PeerListReply"
	}
	return "unknown"
}
