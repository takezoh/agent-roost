package state

import (
	"fmt"
	"time"
)

// PeerMessage is one message in a frame's inbox, sent from another session.
type PeerMessage struct {
	ID        string
	From      FrameID
	Text      string
	ReplyTo   string // optional; ID of a prior PeerMessage this replies to
	SentAt    time.Time
	Delivered bool
}

// peerShortID returns the first 8 characters of a FrameID string.
func peerShortID(id FrameID) string {
	s := string(id)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// formatPeerMessage returns the text to inject into the recipient's pane.
// Format:
//
//	[peer-msg from=<short-id> (<summary>)]
//	<text>
func formatPeerMessage(from FrameID, summary, text string) string {
	if summary != "" {
		return fmt.Sprintf("[peer-msg from=%s (%s)]\n%s", peerShortID(from), summary, text)
	}
	return fmt.Sprintf("[peer-msg from=%s]\n%s", peerShortID(from), text)
}

// PeerDrainInboxParams is the JSON payload for peer.drain_inbox.
type PeerDrainInboxParams struct {
	FromFrameID string `json:"from_frame_id"`
}

func (p PeerDrainInboxParams) TargetFrameID() FrameID { return FrameID(p.FromFrameID) }

// PeerDrainInboxReply is the response body for peer.drain_inbox.
type PeerDrainInboxReply struct {
	Messages []PeerMessage `json:"messages"`
}

// PeerMessagePayload is the broadcast payload for EffBroadcastEvent{Name: "peer-message"}.
type PeerMessagePayload struct {
	ToSessionID SessionID
	FromFrameID FrameID
	Text        string
	SentAt      time.Time
}
