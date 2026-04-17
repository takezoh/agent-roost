package notify

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Notifier sends desktop toast notifications.
// It detects available backends at construction time (PowerShell, notify-send,
// osascript) and routes Send() to the first available one. If none is found,
// Send is a no-op and New returns a zero-cost Notifier with nil error.
type Notifier struct {
	psPath     string // "" → no PowerShell backend
	winPath    string // Windows path to scripts/notify.ps1 under dataDir
	nativeSend func(ctx context.Context, title, body string) error
}

// New prepares the appropriate notification backend for the current platform
// and returns a Notifier. Priority order: PowerShell (WSL/Windows) →
// notify-send (Linux) → osascript (macOS) → no-op.
func New(ctx context.Context, dataDir string) (*Notifier, error) {
	// WSL / Windows: PowerShell Toast
	if ps, err := exec.LookPath("powershell.exe"); err == nil {
		winPath, err := installScript(ctx, dataDir)
		if err != nil {
			return nil, err
		}
		return &Notifier{psPath: ps, winPath: winPath}, nil
	}
	// Linux native: notify-send (libnotify)
	if ns, err := exec.LookPath("notify-send"); err == nil {
		return &Notifier{nativeSend: notifySendBackend(ns)}, nil
	}
	// macOS: osascript
	if osa, err := exec.LookPath("osascript"); err == nil {
		return &Notifier{nativeSend: osascriptBackend(osa)}, nil
	}
	// no-op: no notification system found
	return &Notifier{}, nil
}

// Send dispatches a toast notification. It is safe to call concurrently.
// Returns nil when no backend is available.
func (n *Notifier) Send(ctx context.Context, title, body string) error {
	if n.psPath != "" {
		return sendToast(ctx, n.psPath, n.winPath, title, body)
	}
	if n.nativeSend != nil {
		return n.nativeSend(ctx, title, body)
	}
	return nil
}

// HasBackend reports whether any notification backend is configured.
func (n *Notifier) HasBackend() bool {
	return n.psPath != "" || n.nativeSend != nil
}

func notifySendBackend(path string) func(context.Context, string, string) error {
	return func(ctx context.Context, title, body string) error {
		out, err := exec.CommandContext(ctx, path, "--urgency=normal", title, body).CombinedOutput()
		if err != nil {
			return fmt.Errorf("notify-send: %w: %s", err, out)
		}
		return nil
	}
}

func osascriptBackend(path string) func(context.Context, string, string) error {
	return func(ctx context.Context, title, body string) error {
		script := fmt.Sprintf(`display notification "%s" with title "%s"`,
			escapeAppleScript(body), escapeAppleScript(title))
		out, err := exec.CommandContext(ctx, path, "-e", script).CombinedOutput()
		if err != nil {
			return fmt.Errorf("osascript: %w: %s", err, out)
		}
		return nil
	}
}

// escapeAppleScript escapes backslashes and double-quotes for use inside
// an AppleScript double-quoted string literal.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
