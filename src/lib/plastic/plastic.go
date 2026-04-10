package plastic

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var (
	cmdOnce  sync.Once
	cmdFound bool
)

// DetectBranch returns the current Plastic SCM branch name for the given
// directory. Returns an empty string if the directory is not a Plastic
// workspace or detection failed.
func DetectBranch(dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("cm"); cmdFound = err == nil })
	if !cmdFound {
		return ""
	}
	if !hasPlasticDir(dir) {
		return ""
	}
	cmd := exec.Command("cm", "status", "--machinereadable", "--header")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return ParseBranchFromStatus(string(out))
}

func hasPlasticDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".plastic"))
	return err == nil
}

// ParseBranchFromStatus extracts the branch path from cm status
// --machinereadable --header output. The header line contains a
// changeset spec in the format:
//
//	cs:N@br:/main/feature@repo@server:port
//
// It returns the branch path (e.g. "/main/feature") or empty string
// if parsing fails.
func ParseBranchFromStatus(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		brIdx := strings.Index(line, "@br:")
		if brIdx < 0 {
			continue
		}
		rest := line[brIdx+len("@br:"):]
		atIdx := strings.Index(rest, "@")
		if atIdx < 0 {
			return rest
		}
		return rest[:atIdx]
	}
	return ""
}
