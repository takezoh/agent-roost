package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/takezoh/agent-roost/config"
	libnotify "github.com/takezoh/agent-roost/lib/notify"
)

func init() {
	Register("notify", "Send a desktop notification", RunNotify)
}

// RunNotify implements `roost notify --title <t> --body <b>`.
// Dispatches an OS desktop notification using the available backend
// (PowerShell, notify-send, or osascript). Falls back to stdout JSON
// when no notification backend is available.
func RunNotify(args []string) error {
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	title := fs.String("title", "", "notification title")
	body := fs.String("body", "", "notification body")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *title == "" && *body == "" {
		fmt.Fprintln(os.Stderr, "usage: roost notify --title <title> [--body <body>]")
		return fmt.Errorf("notify: --title or --body required")
	}

	dataDir := resolveNotifyDataDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n, err := libnotify.New(ctx, dataDir)
	if err != nil {
		return fmt.Errorf("notify: init backend: %w", err)
	}

	if !n.HasBackend() {
		return notifyStdout(*title, *body)
	}
	if err := n.Send(ctx, *title, *body); err != nil {
		return fmt.Errorf("notify: send: %w", err)
	}
	return nil
}

// notifyStdout prints the notification as JSON to stdout. Used when no
// OS notification backend is available so scripts can still consume it.
func notifyStdout(title, body string) error {
	return json.NewEncoder(os.Stdout).Encode(map[string]string{
		"title": title,
		"body":  body,
	})
}

func resolveNotifyDataDir() string {
	cfg, err := config.Load()
	if err != nil {
		return filepath.Join(os.TempDir(), "roost")
	}
	return cfg.ResolveDataDir()
}
