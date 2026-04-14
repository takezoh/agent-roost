package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"golang.org/x/term"
)

func init() {
	Register("push", "Push a driver frame onto the current session", RunPush)
}

// RunPush implements `roost push <command>`.
// Reads ROOST_SESSION_ID from the environment (required), reads stdin if piped,
// then asks the daemon to push a new driver frame onto the session.
func RunPush(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: roost push <command>")
		return errors.New("push: missing command")
	}
	command := args[0]

	sid := os.Getenv("ROOST_SESSION_ID")
	if sid == "" {
		return errors.New("push: ROOST_SESSION_ID is not set (must be invoked inside a roost session)")
	}

	var input []byte
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		var err error
		input, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("push: read stdin: %w", err)
		}
		if len(input) == 0 {
			input = nil
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("push: config load: %w", err)
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		return fmt.Errorf("push: dial: %w", err)
	}
	defer client.Close()

	if err := client.PushDriver(sid, command, input); err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}
