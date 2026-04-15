package notify

import (
	"context"
	"os/exec"
)

// Notifier sends desktop toast notifications.
// On platforms without powershell.exe in PATH, Send is a no-op and
// New returns a zero-cost Notifier with nil error.
type Notifier struct {
	psPath  string // "" → unsupported platform, Send is a no-op
	winPath string // Windows path to scripts/notify.ps1 under dataDir
}

// New prepares the notify script under <dataDir>/scripts/ and returns a
// Notifier. The script is written on every call so that an updated binary
// always reflects the latest embedded version. If powershell.exe is not on
// PATH, New returns a no-op Notifier with nil error.
func New(ctx context.Context, dataDir string) (*Notifier, error) {
	psPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		return &Notifier{}, nil
	}
	winPath, err := installScript(ctx, dataDir)
	if err != nil {
		return nil, err
	}
	return &Notifier{psPath: psPath, winPath: winPath}, nil
}

// Send dispatches a toast notification. It is safe to call concurrently.
// On platforms without powershell.exe, Send is a no-op and returns nil.
func (n *Notifier) Send(ctx context.Context, title, body string) error {
	if n.psPath == "" {
		return nil
	}
	return sendToast(ctx, n.psPath, n.winPath, title, body)
}
