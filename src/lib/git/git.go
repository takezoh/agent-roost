package git

import (
	"net/url"
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

// DetectRemoteHost returns the hostname of the "origin" remote
// (e.g. "github.com"). Returns "" if the remote cannot be determined.
func DetectRemoteHost(dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return parseHost(strings.TrimSpace(string(out)))
}

// parseHost extracts the hostname from a git remote URL.
// Supports SSH (git@host:path) and HTTPS (https://host/path) forms.
func parseHost(raw string) string {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return ""
		}
		return strings.ToLower(u.Hostname())
	}
	// SSH form: git@github.com:user/repo.git
	if at := strings.Index(raw, "@"); at >= 0 {
		rest := raw[at+1:]
		if colon := strings.Index(rest, ":"); colon > 0 {
			return strings.ToLower(rest[:colon])
		}
	}
	return ""
}

// hasGitDir checks for .git (directory or file for worktrees).
func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
