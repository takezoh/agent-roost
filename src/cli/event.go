package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/proto"
	"golang.org/x/term"
)

func init() {
	Register("event", "Send an event to the daemon", RunEvent)
}

// RunEvent implements `roost event <eventType>`.
// Reads stdin (if piped), captures ROOST_FRAME_ID and a timestamp,
// then sends a CmdEvent to the daemon.
func RunEvent(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: roost event <event-type>")
		return errors.New("event: missing event type")
	}
	eventType := args[0]

	senderID := os.Getenv("ROOST_FRAME_ID")
	if senderID == "" {
		slog.Warn("event: ROOST_FRAME_ID not set; dropping event", "type", eventType)
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

	if err := sendToDaemon(eventType, ts, senderID, json.RawMessage(input)); err != nil {
		slog.Warn("event: send failed", "err", err)
	}
	return nil
}

func sendToDaemon(eventType string, ts time.Time, senderID string, payload json.RawMessage) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config load: %w", err)
	}
	sockPath := filepath.Join(cfg.ResolveDataDir(), "roost.sock")
	client, err := proto.Dial(sockPath)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer client.Close()

	if err := client.SendEvent(eventType, ts, senderID, payload); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}
