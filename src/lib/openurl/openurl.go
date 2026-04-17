// Package openurl launches the host's default handler for a URL or
// local path. It is a thin wrapper over xdg-open / open / explorer.exe
// with a WSL-aware fallback that hands paths to Windows explorer so the
// user sees a native file manager even when roost runs inside WSL.
package openurl

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Open launches the host handler for target (file path or URL) and
// returns without waiting for the child to exit. The child is detached
// so it does not block the TUI.
func Open(target string) error {
	return openWith(currentEnv(), target)
}

type env struct {
	goos string
	wsl  bool
}

func currentEnv() env {
	return env{goos: runtime.GOOS, wsl: isWSL()}
}

// command picks the executable and arguments to launch for target under
// the given environment. Exported shape via openCommand for tests.
func (e env) command(target string) (string, []string, error) {
	if target == "" {
		return "", nil, fmt.Errorf("openurl: empty target")
	}
	switch e.goos {
	case "darwin":
		return "open", []string{target}, nil
	case "windows":
		return "explorer.exe", []string{target}, nil
	case "linux":
		if e.wsl {
			arg := target
			if path, ok := fileTargetToPath(target); ok {
				if win, err := wslToWindowsPath(path); err == nil {
					arg = win
				}
			}
			return "explorer.exe", []string{arg}, nil
		}
		return "xdg-open", []string{target}, nil
	default:
		return "", nil, fmt.Errorf("openurl: unsupported GOOS %q", e.goos)
	}
}

func openWith(e env, target string) error {
	name, args, err := e.command(target)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(context.Background(), name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("openurl: start %s: %w", name, err)
	}
	// Release the child so it does not turn into a zombie and does not
	// block the caller. We intentionally ignore the exit code.
	go func() { _ = cmd.Wait() }()
	return nil
}

func fileTargetToPath(target string) (string, bool) {
	switch {
	case strings.HasPrefix(target, "file://localhost"):
		return strings.TrimPrefix(target, "file://localhost"), true
	case strings.HasPrefix(target, "file:///"):
		return strings.TrimPrefix(target, "file://"), true
	case strings.HasPrefix(target, "/"):
		return target, true
	}
	return "", false
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err == nil {
		return true
	}
	return false
}

func wslToWindowsPath(p string) (string, error) {
	out, err := exec.Command("wslpath", "-w", p).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
