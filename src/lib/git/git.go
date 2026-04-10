package git

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

// DetectBranch returns the current git branch name for the given directory.
// Returns an empty string if the directory is not a git repository.
func DetectBranch(dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound {
		return ""
	}
	if !hasGitDir(dir) {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// hasGitDir checks for .git (directory or file for worktrees).
func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
