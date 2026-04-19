package peers

import (
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/proto"
)

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
