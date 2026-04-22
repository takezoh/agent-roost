package cli

import (
	"fmt"
	"path/filepath"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
)

func init() {
	Register("statusline-click", "Notify daemon of a status-bar click (internal tmux binding)", runStatusLineClick)
}

// runStatusLineClick implements `roost statusline-click [range_name]`.
// Called by the tmux MouseDown1Status binding:
//
//	roost statusline-click #{mouse_status_range}
//
// range_name is the tmux mouse_status_range value; empty means no named region was hit.
func runStatusLineClick(args []string) error {
	rangeName := ""
	if len(args) > 0 {
		rangeName = args[0]
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("statusline-click: config load: %w", err)
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		return fmt.Errorf("statusline-click: dial: %w", err)
	}
	defer client.Close()
	return client.StatusLineClick(rangeName)
}
