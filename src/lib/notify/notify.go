package notify

import (
	"context"
	"os/exec"
)

// Send sends a desktop notification with the given title and body.
// On platforms where notification is unsupported (no powershell.exe in PATH),
// it returns nil silently — callers need not check for platform availability.
func Send(ctx context.Context, title, body string) error {
	psPath, err := exec.LookPath("powershell.exe")
	if err != nil {
		return nil
	}
	return sendWindowsToast(ctx, psPath, title, body)
}
