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

// IsWorktree reports whether dir is a linked git worktree (not the
// main working tree). In a linked worktree .git is a regular file
// containing a gitdir pointer; in the main tree it is a directory.
func IsWorktree(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

// DetectMainBranch returns the branch checked out in the main working
// tree. This is useful when called from a linked worktree to show
// which branch the parent repo is on. Returns "" on any failure.
func DetectMainBranch(dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return ""
	}
	return parseMainBranch(string(out))
}

// parseMainBranch extracts the branch name from the first entry of
// `git worktree list --porcelain` output. The first entry is always
// the main working tree.
func parseMainBranch(output string) string {
	const prefix = "branch refs/heads/"
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			// End of first entry — stop scanning.
			return ""
		}
		if strings.HasPrefix(line, prefix) {
			return line[len(prefix):]
		}
	}
	return ""
}

// hasGitDir checks for .git (directory or file for worktrees).
func hasGitDir(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}
