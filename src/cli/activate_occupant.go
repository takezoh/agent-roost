package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/takezoh/agent-roost/proto"
)

func init() {
	Register("activate-occupant", "Switch the main pane occupant (main|log)", runActivateOccupant)
}

// runActivateOccupant implements `roost activate-occupant <kind>`.
// kind must be "main" or "log". Called by the prefix+l tmux keybinding.
func runActivateOccupant(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: roost activate-occupant <main|log>")
		return errors.New("activate-occupant: missing kind")
	}
	kind := args[0]
	if kind != "main" && kind != "log" {
		return fmt.Errorf("activate-occupant: unknown kind %q (want main or log)", kind)
	}
	sockPath, err := resolveSocketPath()
	if err != nil {
		return fmt.Errorf("activate-occupant: %w", err)
	}
	client, err := proto.Dial(sockPath)
	if err != nil {
		return fmt.Errorf("activate-occupant: dial: %w", err)
	}
	defer client.Close()
	return client.ActivateOccupant(kind, "", "")
}
