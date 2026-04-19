package peers

import (
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takezoh/agent-roost/proto"
)

// fakePeerClient is a test double for peerClient.
type fakePeerClient struct {
	peers    []proto.PeerPeerInfo
	messages []proto.PeerMessage
	listErr  error
	sendErr  error
	summErr  error
	drainErr error
	closed   bool

	lastFromID  string
	lastToID    string
	lastText    string
	lastReplyTo string
	lastScope   string
	lastSummary string
}

func (f *fakePeerClient) PeerList(fromFrameID, scope string) ([]proto.PeerPeerInfo, error) {
	f.lastFromID = fromFrameID
	f.lastScope = scope
	return f.peers, f.listErr
}

func (f *fakePeerClient) PeerSend(fromFrameID, toFrameID, text, replyTo string) error {
	f.lastFromID = fromFrameID
	f.lastToID = toFrameID
	f.lastText = text
	f.lastReplyTo = replyTo
	return f.sendErr
}

func (f *fakePeerClient) PeerSetSummary(fromFrameID, summary string) error {
	f.lastFromID = fromFrameID
	f.lastSummary = summary
	return f.summErr
}

func (f *fakePeerClient) PeerDrainInbox(frameID string) ([]proto.PeerMessage, error) {
	f.lastFromID = frameID
	return f.messages, f.drainErr
}

func (f *fakePeerClient) Close() error {
	f.closed = true
	return nil
}

func makeDialer(fc *fakePeerClient) dialer {
	return func() (peerClient, error) { return fc, nil }
}

func makeFailDialer(err error) dialer {
	return func() (peerClient, error) { return nil, err }
}

// --- handleListPeers ---

func TestHandleListPeers_Success(t *testing.T) {
	fc := &fakePeerClient{
		peers: []proto.PeerPeerInfo{{FrameID: "f1", Driver: "claude", Status: "working"}},
	}
	res, err := handleListPeers(makeDialer(fc), "caller", "workspace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fc.closed {
		t.Error("client.Close not called")
	}
	if fc.lastScope != "workspace" {
		t.Errorf("scope = %q, want %q", fc.lastScope, "workspace")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "f1") {
		t.Errorf("response JSON should contain frame_id f1, got: %s", text)
	}
}

func TestHandleListPeers_DialError(t *testing.T) {
	_, err := handleListPeers(makeFailDialer(errors.New("refused")), "caller", "workspace")
	if err == nil || !strings.Contains(err.Error(), "dial daemon") {
		t.Fatalf("want dial error, got %v", err)
	}
}

func TestHandleListPeers_ClientError(t *testing.T) {
	fc := &fakePeerClient{listErr: errors.New("rpc fail")}
	_, err := handleListPeers(makeDialer(fc), "caller", "workspace")
	if err == nil || !strings.Contains(err.Error(), "peer.list") {
		t.Fatalf("want peer.list error, got %v", err)
	}
	if !fc.closed {
		t.Error("client.Close not called on error")
	}
}

// --- handlePeerSend ---

func TestHandlePeerSend_Success(t *testing.T) {
	fc := &fakePeerClient{}
	res, err := handlePeerSend(makeDialer(fc), "from", "to", "hello", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.lastToID != "to" || fc.lastText != "hello" {
		t.Errorf("args not passed: toID=%q text=%q", fc.lastToID, fc.lastText)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if text != "sent" {
		t.Errorf("got %q, want %q", text, "sent")
	}
}

func TestHandlePeerSend_DialError(t *testing.T) {
	_, err := handlePeerSend(makeFailDialer(errors.New("refused")), "from", "to", "hi", "")
	if err == nil || !strings.Contains(err.Error(), "dial daemon") {
		t.Fatalf("want dial error, got %v", err)
	}
}

func TestHandlePeerSend_ClientError(t *testing.T) {
	fc := &fakePeerClient{sendErr: errors.New("not found")}
	_, err := handlePeerSend(makeDialer(fc), "from", "to", "hi", "")
	if err == nil || !strings.Contains(err.Error(), "peer.send") {
		t.Fatalf("want peer.send error, got %v", err)
	}
}

// --- handleSetSummary ---

func TestHandleSetSummary_Success(t *testing.T) {
	fc := &fakePeerClient{}
	res, err := handleSetSummary(makeDialer(fc), "frame1", "working on auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.lastSummary != "working on auth" {
		t.Errorf("summary = %q", fc.lastSummary)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if text != "ok" {
		t.Errorf("got %q, want ok", text)
	}
}

func TestHandleSetSummary_ClientError(t *testing.T) {
	fc := &fakePeerClient{summErr: errors.New("gone")}
	_, err := handleSetSummary(makeDialer(fc), "frame1", "summary")
	if err == nil || !strings.Contains(err.Error(), "peer.set_summary") {
		t.Fatalf("want peer.set_summary error, got %v", err)
	}
}

// --- handleCheckMessages ---

func TestHandleCheckMessages_Success(t *testing.T) {
	fc := &fakePeerClient{
		messages: []proto.PeerMessage{{ID: "m1", From: "f2", Text: "ping"}},
	}
	res, err := handleCheckMessages(makeDialer(fc), "frame1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"count":1`) {
		t.Errorf("expected count:1 in response, got: %s", text)
	}
	if !strings.Contains(text, "ping") {
		t.Errorf("expected message text in response, got: %s", text)
	}
}

func TestHandleCheckMessages_Empty(t *testing.T) {
	fc := &fakePeerClient{}
	res, err := handleCheckMessages(makeDialer(fc), "frame1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, `"count":0`) {
		t.Errorf("expected count:0, got: %s", text)
	}
}

func TestHandleCheckMessages_ClientError(t *testing.T) {
	fc := &fakePeerClient{drainErr: errors.New("timeout")}
	_, err := handleCheckMessages(makeDialer(fc), "frame1")
	if err == nil || !strings.Contains(err.Error(), "peer.drain_inbox") {
		t.Fatalf("want drain_inbox error, got %v", err)
	}
}
