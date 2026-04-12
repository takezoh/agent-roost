package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/takezoh/agent-roost/cli"
	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"golang.org/x/term"
)

func init() {
	cli.Register("event", "Send an event to the daemon", Run)
}

// Run implements `roost event <eventType>`.
// Reads stdin (if piped), captures ROOST_SESSION_ID and a timestamp,
// then sends a CmdEvent to the daemon.
func Run(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: roost event <event-type>")
		return errors.New("event: missing event type")
	}
	eventType := args[0]

	senderID := os.Getenv("ROOST_SESSION_ID")
	if senderID == "" {
		return nil
	}
	ts := time.Now()

	var input []byte
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		input, _ = io.ReadAll(os.Stdin)
	}

	slog.Debug("event",
		"type", eventType,
		"sender", senderID,
		"input_len", len(input),
	)

	cfg, err := config.Load()
	if err != nil {
		slog.Warn("event: config load failed", "err", err)
		return nil
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		slog.Warn("event: dial failed", "sock", sockPath, "err", err)
		return nil
	}
	defer client.Close()

	if err := client.SendEvent(eventType, ts, senderID, json.RawMessage(input)); err != nil {
		slog.Warn("event: send failed", "type", eventType, "sender", senderID, "err", err)
	}
	return nil
}
