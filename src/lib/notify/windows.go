package notify

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed notify.ps1
var notifyScript []byte

// installScript writes the embedded script to <dataDir>/scripts/notify.ps1
// and returns its Windows path. The file is always overwritten so that an
// updated binary automatically deploys the latest script version.
func installScript(ctx context.Context, dataDir string) (string, error) {
	scriptDir := filepath.Join(dataDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir scripts: %w", err)
	}
	scriptPath := filepath.Join(scriptDir, "notify.ps1")
	if err := os.WriteFile(scriptPath, notifyScript, 0o644); err != nil {
		return "", fmt.Errorf("write script: %w", err)
	}
	winPath, err := toWindowsPath(ctx, scriptPath)
	if err != nil {
		return "", fmt.Errorf("wslpath: %w", err)
	}
	return winPath, nil
}

func sendToast(ctx context.Context, psPath, winPath, title, body string) error {
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
