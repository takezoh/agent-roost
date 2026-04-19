package peers

import (
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/proto"
)

// peerClient is the minimal proto.Client surface the MCP handlers need.
// *proto.Client satisfies this interface.
type peerClient interface {
	PeerList(fromFrameID, scope string) ([]proto.PeerPeerInfo, error)
	PeerSend(fromFrameID, toFrameID, text, replyTo string) error
	PeerSetSummary(fromFrameID, summary string) error
	PeerDrainInbox(frameID string) ([]proto.PeerMessage, error)
	Close() error
}

// dialer opens a peerClient to the roost daemon.
type dialer func() (peerClient, error)

// defaultDialer returns a dialer backed by the real daemon socket.
func defaultDialer() dialer {
	return func() (peerClient, error) {
		return dialDaemon()
	}
}

// dialDaemon opens a proto.Client to the roost daemon socket.
func dialDaemon() (*proto.Client, error) {
	socketPath := os.Getenv("ROOST_SOCKET")
	if socketPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		socketPath = filepath.Join(home, ".roost", "data", "roost.sock")
	}
	return proto.Dial(socketPath)
}

// callerFrameID returns the caller's frame ID from the environment.
func callerFrameID() string {
	return os.Getenv("ROOST_FRAME_ID")
}
