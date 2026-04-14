package notify

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

//go:embed notify.ps1
var notifyScript []byte

func sendWindowsToast(ctx context.Context, psPath, title, body string) error {
	tmp, err := writeScriptTempFile()
	if err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(tmp)

	winPath, err := toWindowsPath(ctx, tmp)
	if err != nil {
		return fmt.Errorf("wslpath: %w", err)
	}

	cmd := exec.CommandContext(ctx, psPath,
		"-NoProfile", "-ExecutionPolicy", "Bypass",
		"-File", winPath,
		"-Title", xmlEscape(title),
		"-Body", xmlEscape(body),
	)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("powershell: %w: %s", err, out)
	}
	return nil
}

func writeScriptTempFile() (string, error) {
	f, err := os.CreateTemp("", "roost-notify-*.ps1")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(notifyScript); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

func toWindowsPath(ctx context.Context, linuxPath string) (string, error) {
	out, err := exec.CommandContext(ctx, "wslpath", "-w", linuxPath).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// xmlEscape escapes the five XML special characters.
func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}

