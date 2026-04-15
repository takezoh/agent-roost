package vcs

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestDetectBranchGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "test-branch")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")

	r := DetectBranch(context.Background(), dir)
	if r.Branch != "test-branch" {
		t.Errorf("Branch = %q, want %q", r.Branch, "test-branch")
	}
	if r.Background != gitBG {
		t.Errorf("Background = %q, want %q", r.Background, gitBG)
	}
	if r.Foreground != defaultFG {
		t.Errorf("Foreground = %q, want %q", r.Foreground, defaultFG)
	}
}

func TestDetectBranchGitHubRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:user/repo.git")

	r := DetectBranch(context.Background(), dir)
	if r.Background != hostColors["github.com"] {
		t.Errorf("Background = %q, want %q", r.Background, hostColors["github.com"])
	}
}

func TestDetectBranchGitLabRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")
	gitRun(t, dir, "remote", "add", "origin", "https://gitlab.com/user/repo.git")

	r := DetectBranch(context.Background(), dir)
	if r.Background != hostColors["gitlab.com"] {
		t.Errorf("Background = %q, want %q", r.Background, hostColors["gitlab.com"])
	}
}

func TestDetectBranchUnknownRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")
	gitRun(t, dir, "remote", "add", "origin", "https://selfhosted.example.com/repo.git")

	r := DetectBranch(context.Background(), dir)
	if r.Background != gitBG {
		t.Errorf("Background = %q, want %q (fallback)", r.Background, gitBG)
	}
}

func TestDetectBranchNoRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")

	r := DetectBranch(context.Background(), dir)
	if r.Background != gitBG {
		t.Errorf("Background = %q, want %q (fallback)", r.Background, gitBG)
	}
}

func TestDetectBranch_Worktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")
	gitRun(t, dir, "branch", "feature")
	wtDir := t.TempDir()
	gitRun(t, dir, "worktree", "add", wtDir, "feature")

	r := DetectBranch(context.Background(), wtDir)
	if r.Branch != "feature" {
		t.Errorf("Branch = %q, want %q", r.Branch, "feature")
	}
	if !r.IsWorktree {
		t.Error("IsWorktree = false, want true")
	}
	if r.ParentBranch != "main" {
		t.Errorf("ParentBranch = %q, want %q", r.ParentBranch, "main")
	}
}

func TestDetectBranch_MainRepo_NotWorktree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")

	r := DetectBranch(context.Background(), dir)
	if r.IsWorktree {
		t.Error("IsWorktree = true, want false")
	}
	if r.ParentBranch != "" {
		t.Errorf("ParentBranch = %q, want empty", r.ParentBranch)
	}
}

func TestDetectBranchNoVCS(t *testing.T) {
	dir := t.TempDir()
	r := DetectBranch(context.Background(), dir)
	if r.Branch != "" {
		t.Errorf("Branch = %q, want empty", r.Branch)
	}
	if r.Background != "" {
		t.Errorf("Background = %q, want empty", r.Background)
	}
}
