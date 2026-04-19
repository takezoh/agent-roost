package state

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/takezoh/agent-roost/features"
)

// Event names for peer operations.
const (
	EventPeerSend       = "peer.send"
	EventPeerList       = "peer.list"
	EventPeerSetSummary = "peer.set_summary"
	EventPeerDrainInbox = "peer.drain_inbox"
)

func init() {
	RegisterFrameEvent[PeerSendParams](EventPeerSend, reducePeerSend)
	RegisterEvent[PeerListParams](EventPeerList, reducePeerList)
	RegisterFrameEvent[PeerSetSummaryParams](EventPeerSetSummary, reducePeerSetSummary)
	RegisterFrameEvent[PeerDrainInboxParams](EventPeerDrainInbox, reducePeerDrainInbox)
}

// PeerSendParams is the JSON payload for peer.send.
type PeerSendParams struct {
	ToFrameID   string `json:"to"`
	Text        string `json:"text"`
	ReplyTo     string `json:"reply_to,omitempty"`
	FromFrameID string `json:"from"` // set by daemon_client from ROOST_FRAME_ID
}

func (p PeerSendParams) TargetFrameID() FrameID { return FrameID(p.ToFrameID) }

// PeerListParams is the JSON payload for peer.list.
type PeerListParams struct {
	Scope       string `json:"scope,omitempty"`         // "workspace", "project", "all"
	FromFrameID string `json:"from_frame_id,omitempty"` // filled by daemon_client
}

// PeerSetSummaryParams is the JSON payload for peer.set_summary.
type PeerSetSummaryParams struct {
	Summary     string `json:"summary"`
	FromFrameID string `json:"from_frame_id"` // filled by daemon_client
}

func (p PeerSetSummaryParams) TargetFrameID() FrameID { return FrameID(p.FromFrameID) }

// PeerInfo is a summary of one peer frame returned by peer.list.
type PeerInfo struct {
	FrameID    string `json:"frame_id"`
	SessionID  string `json:"session_id"`
	Driver     string `json:"driver"`
	Project    string `json:"project"`
	Workspace  string `json:"workspace"`
	Summary    string `json:"summary"`
	Status     Status `json:"status"`
	InboxCount int    `json:"inbox_count"`
}

// PeerListReply is the response body for peer.list.
type PeerListReply struct {
	Peers []PeerInfo `json:"peers"`
}

func reducePeerSend(s State, connID ConnID, reqID string, ctx FrameCtx, p PeerSendParams) (State, []Effect) {
	if !s.Features.On(features.Peers) {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "peers feature is disabled")}
	}
	toFrameID := ctx.Frame.ID
	fromFrameID := FrameID(p.FromFrameID)

	msg := PeerMessage{
		ID:      allocMsgID(),
		From:    fromFrameID,
		Text:    p.Text,
		ReplyTo: p.ReplyTo,
		SentAt:  s.Now,
	}

	var effs []Effect

	if ctx.Driver != nil && !wasBlockingStatus(ctx.Status) {
		// Driver known and not busy — inject immediately. Unknown-driver frames queue below.
		summary := peerSummaryOf(s, fromFrameID)
		effs = append(effs, EffInjectPrompt{
			FrameID: toFrameID,
			Text:    formatPeerMessage(fromFrameID, summary, p.Text),
		})
		msg.Delivered = true
	}

	if !msg.Delivered {
		// Queue into inbox for later drain.
		toFrame := ctx.Frame
		toFrame.PeerInbox = append(append([]PeerMessage(nil), toFrame.PeerInbox...), msg)
		toSess := ctx.Session
		toSess.Frames = append([]SessionFrame(nil), toSess.Frames...)
		toSess.Frames[ctx.FrameIndex] = toFrame
		s.Sessions = cloneSessions(s.Sessions)
		s.Sessions[ctx.SessionID] = toSess
	}

	// Log the message to both frames' event logs.
	preview := first40(p.Text)
	effs = append(effs,
		EffEventLogAppend{FrameID: fromFrameID, Line: "[peer→to=" + peerShortID(toFrameID) + "]: " + preview},
		EffEventLogAppend{FrameID: toFrameID, Line: "[peer←from=" + peerShortID(fromFrameID) + "]: " + preview},
		EffBroadcastEvent{
			Name: "peer-message",
			Payload: PeerMessagePayload{
				ToSessionID: ctx.SessionID,
				FromFrameID: fromFrameID,
				Text:        p.Text,
				SentAt:      s.Now,
			},
		},
		EffBroadcastSessionsChanged{},
		okResp(connID, reqID, nil),
	)
	return s, effs
}

func reducePeerList(s State, connID ConnID, reqID string, p PeerListParams) (State, []Effect) {
	if !s.Features.On(features.Peers) {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "peers feature is disabled")}
	}
	fromFrameID := FrameID(p.FromFrameID)
	scope := p.Scope
	if scope == "" {
		scope = "workspace"
	}

	_, fromSess, _, fromOK := findFrame(s, fromFrameID)

	var peers []PeerInfo
	for sessID, sess := range s.Sessions {
		frame, ok := activeFrame(sess)
		if !ok {
			continue
		}
		if frame.ID == fromFrameID {
			continue
		}

		if fromOK {
			switch scope {
			case "workspace":
				if !sameWorkspace(fromSess.Project, sess.Project) {
					continue
				}
			case "project":
				if fromSess.Project != sess.Project {
					continue
				}
			// "all": no filter
			}
		}

		drv := GetDriver(frame.Command)
		var status Status
		if drv != nil {
			status = drv.Status(frame.Driver)
		}

		peers = append(peers, PeerInfo{
			FrameID:    string(frame.ID),
			SessionID:  string(sessID),
			Driver:     frame.Command,
			Project:    sess.Project,
			Workspace:  workspaceOf(sess.Project),
			Summary:    peerSummaryOf(s, frame.ID),
			Status:     status,
			InboxCount: len(frame.PeerInbox),
		})
	}

	return s, []Effect{okResp(connID, reqID, PeerListReply{Peers: peers})}
}

func reducePeerSetSummary(s State, connID ConnID, reqID string, ctx FrameCtx, p PeerSetSummaryParams) (State, []Effect) {
	if !s.Features.On(features.Peers) {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "peers feature is disabled")}
	}
	sess := ctx.Session
	sess.Frames = append([]SessionFrame(nil), sess.Frames...)
	sess.Frames[ctx.FrameIndex].PeerSummary = p.Summary
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[ctx.SessionID] = sess
	return s, []Effect{EffBroadcastSessionsChanged{}, okResp(connID, reqID, nil)}
}

// drainPeerInbox drains queued messages for a frame that has just become
// idle (i.e. oldStatus was busy, newStatus is not). Returns the updated
// state and a slice of EffInjectPrompt effects (one per drained message).
func drainPeerInbox(s State, sessID SessionID, frameID FrameID, oldStatus, newStatus Status) (State, []Effect) {
	if !wasBlockingStatus(oldStatus) || wasBlockingStatus(newStatus) {
		return s, nil
	}

	sess, ok := s.Sessions[sessID]
	if !ok {
		return s, nil
	}
	idx := findFrameIndex(sess, frameID)
	if idx < 0 {
		return s, nil
	}
	frame := sess.Frames[idx]
	if len(frame.PeerInbox) == 0 {
		return s, nil
	}

	effs := make([]Effect, 0, len(frame.PeerInbox))
	for _, msg := range frame.PeerInbox {
		summary := peerSummaryOf(s, msg.From)
		effs = append(effs, EffInjectPrompt{
			FrameID: frameID,
			Text:    formatPeerMessage(msg.From, summary, msg.Text),
		})
	}

	// Clear the inbox.
	sess.Frames = append([]SessionFrame(nil), sess.Frames...)
	sess.Frames[idx].PeerInbox = nil
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[sessID] = sess

	return s, effs
}

// wasBlockingStatus reports whether a status is one that prevents
// immediate peer message delivery (running/waiting/pending).
func wasBlockingStatus(st Status) bool {
	return st == StatusRunning || st == StatusWaiting || st == StatusPending
}

// peerSummaryOf returns the PeerSummary for a frame, falling back to the
// driver view's Card.Subtitle (first 60 chars) if PeerSummary is empty.
func peerSummaryOf(s State, frameID FrameID) string {
	_, sess, idx, ok := findFrame(s, frameID)
	if !ok {
		return ""
	}
	frame := sess.Frames[idx]
	if frame.PeerSummary != "" {
		return frame.PeerSummary
	}
	drv := GetDriver(frame.Command)
	if drv == nil {
		return ""
	}
	subtitle := drv.View(frame.Driver).Card.Subtitle
	runes := []rune(subtitle)
	if len(runes) > 60 {
		return string(runes[:60])
	}
	return subtitle
}

// allocMsgID generates a random 8-character hex message ID.
func allocMsgID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("state: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func reducePeerDrainInbox(s State, connID ConnID, reqID string, ctx FrameCtx, p PeerDrainInboxParams) (State, []Effect) {
	if !s.Features.On(features.Peers) {
		return s, []Effect{errResp(connID, reqID, ErrCodeInvalidArgument, "peers feature is disabled")}
	}
	inbox := ctx.Frame.PeerInbox
	if len(inbox) == 0 {
		return s, []Effect{okResp(connID, reqID, PeerDrainInboxReply{Messages: nil})}
	}
	// Clear inbox.
	frame := ctx.Frame
	frame.PeerInbox = nil
	sess := ctx.Session
	sess.Frames = append([]SessionFrame(nil), sess.Frames...)
	sess.Frames[ctx.FrameIndex] = frame
	s.Sessions = cloneSessions(s.Sessions)
	s.Sessions[ctx.SessionID] = sess
	return s, []Effect{EffBroadcastSessionsChanged{}, okResp(connID, reqID, PeerDrainInboxReply{Messages: inbox})}
}

// first40 returns the first 40 runes of s (or all of s if shorter),
// with newlines replaced by " / " to keep EVENTS log clean.
func first40(s string) string {
	s = strings.ReplaceAll(s, "\n", " / ")
	runes := []rune(s)
	if len(runes) > 40 {
		return string(runes[:40])
	}
	return s
}

// sameWorkspace reports whether two project paths share the same workspace
// root (defined as everything up to the first meaningful path component
// difference — here we use the first two path segments).
func sameWorkspace(a, b string) bool {
	return workspaceOf(a) == workspaceOf(b)
}

// workspaceOf extracts the workspace root from a project path.
// Uses the first two path segments as workspace key if path has >= 3 segments,
// otherwise uses the first segment.
// E.g. "/home/take/proj-a" → "/home/take", "/workspace/proj-a" → "/workspace".
func workspaceOf(project string) string {
	trimmed := strings.TrimLeft(project, "/")
	if trimmed == "" {
		return project
	}
	parts := strings.SplitN(trimmed, "/", 3)
	switch len(parts) {
	case 1:
		return "/" + parts[0]
	case 2:
		return "/" + parts[0]
	default:
		return "/" + parts[0] + "/" + parts[1]
	}
}

