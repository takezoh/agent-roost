package git

import (
	"context"
	"fmt"
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
func DetectBranch(ctx context.Context, dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound {
		return ""
	}
	if !hasGitDir(dir) {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// DetectRemoteHost returns the hostname of the "origin" remote
// (e.g. "github.com"). Returns "" if the remote cannot be determined.
func DetectRemoteHost(ctx context.Context, dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin").Output()
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
func DetectMainBranch(ctx context.Context, dir string) string {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return ""
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "worktree", "list", "--porcelain").Output()
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

// RepoRoot returns the canonical git top-level directory for dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return "", fmt.Errorf("%s is not a git repository", dir)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("resolve git root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func commonGitRoot(ctx context.Context, dir string) (string, error) {
	cmdOnce.Do(func() { _, err := exec.LookPath("git"); cmdFound = err == nil })
	if !cmdFound || !hasGitDir(dir) {
		return "", fmt.Errorf("%s is not a git repository", dir)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return "", fmt.Errorf("resolve git common dir: %w", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out))), nil
}

// CreateWorktree creates a new linked git worktree under
// <repo>/.roost/worktrees/<name> and returns that path.
func CreateWorktree(ctx context.Context, dir, name string) (string, error) {
	root, err := RepoRoot(ctx, dir)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("worktree name required")
	}
	worktreeDir := filepath.Join(root, ".roost", "worktrees", name)
	if _, err := os.Stat(worktreeDir); err == nil {
		return "", fmt.Errorf("worktree already exists: %s", worktreeDir)
	}
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o755); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "add", "-b", name, worktreeDir).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(string(out)))
	}
	return worktreeDir, nil
}

// RemoveWorktree removes a roost-managed git worktree created under
// <repo>/.roost/worktrees/<name>.
func RemoveWorktree(ctx context.Context, path string) error {
	clean := filepath.Clean(path)
	root, err := commonGitRoot(ctx, clean)
	if err != nil {
		return err
	}
	if filepath.Dir(clean) != filepath.Join(root, ".roost", "worktrees") {
		return fmt.Errorf("not a managed worktree path: %s", clean)
	}
	out, err := exec.CommandContext(ctx, "git", "-C", root, "worktree", "remove", "--force", clean).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree remove failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
