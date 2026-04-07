package git

import (
	"os/exec"
	"strings"
)

// DetectBranch returns the current git branch name for the given directory.
// Returns an empty string if the directory is not a git repository.
func DetectBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
