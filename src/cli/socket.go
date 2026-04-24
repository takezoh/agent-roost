package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/config"
)

// resolveSocketPath returns the roost daemon UDS path, preferring the
// ROOST_SOCKET env var when set. Inside a Docker sandbox container the env is
// set to the bind-mounted path (e.g. /tmp/roost.sock) so guest `roost` CLIs
// reach the same host daemon as local invocations.
func resolveSocketPath() (string, error) {
	if s := os.Getenv("ROOST_SOCKET"); s != "" {
		return s, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return "", fmt.Errorf("config load: %w", err)
	}
	return filepath.Join(cfg.ResolveDataDir(), "roost.sock"), nil
}
