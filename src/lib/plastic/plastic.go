package plastic

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var (
	cmdOnce sync.Once
	cmdName string // "cm", "cm.exe", or "" if not found
)

// DetectBranch returns the current Plastic SCM branch name for the given
// directory. Returns an empty string if the directory is not a Plastic
// workspace or detection failed.
func DetectBranch(dir string) string {
	cmdOnce.Do(locateCmd)
	if cmdName == "" {
		return ""
	}
	if findPlasticRoot(dir) == "" {
		return ""
	}
	cmd := exec.Command(cmdName, "wi", "--machinereadable")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return ParseBranchFromWorkspaceInfo(string(out))
}

// locateCmd searches PATH for the Plastic SCM CLI. On non-Windows hosts
// (notably WSL) it falls back to cm.exe so the Windows client reached
// via interop is also usable.
func locateCmd() {
	if _, err := exec.LookPath("cm"); err == nil {
		cmdName = "cm"
		return
	}
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("cm.exe"); err == nil {
			cmdName = "cm.exe"
		}
	}
}

// findPlasticRoot walks up the directory tree from dir looking for a .plastic
// directory. Returns the directory containing .plastic, or "" if not found.
func findPlasticRoot(dir string) string {
	d := dir
	for {
		if _, err := os.Lstat(filepath.Join(d, ".plastic")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// ParseBranchFromWorkspaceInfo extracts the branch path from
// `cm wi --machinereadable` output. The format is:
//
//	BR <branch_path> <repo>@<server>
//
// Returns the branch path (e.g. "/main/feature") or empty string when
// the workspace is not pointing at a branch (e.g. on a label or a
// specific changeset, which use "LB" / "CS" prefixes).
func ParseBranchFromWorkspaceInfo(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "BR ") {
			continue
		}
		rest := line[len("BR "):]
		if sp := strings.IndexByte(rest, ' '); sp >= 0 {
			return rest[:sp]
		}
		return rest
	}
	return ""
}
