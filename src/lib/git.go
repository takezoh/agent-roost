package lib

import (
	"os/exec"
	"strings"
)

// DetectGitBranch returns the current git branch name for the given directory.
// Returns an empty string if the directory is not a git repository.
func DetectGitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
