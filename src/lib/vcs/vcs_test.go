package vcs

import (
	"os"
	"os/exec"
	"testing"
)

func TestDetectBranchGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_NOSYSTEM=1",
			"HOME="+dir,
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	run("init", "-b", "test-branch")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("commit", "--allow-empty", "-m", "init")

	r := DetectBranch(dir)
	if r.Branch != "test-branch" {
		t.Errorf("Branch = %q, want %q", r.Branch, "test-branch")
	}
	if r.VCS != "git" {
		t.Errorf("VCS = %q, want %q", r.VCS, "git")
	}
}

func TestDetectBranchNoVCS(t *testing.T) {
	dir := t.TempDir()
	r := DetectBranch(dir)
	if r.Branch != "" {
		t.Errorf("Branch = %q, want empty", r.Branch)
	}
	if r.VCS != "" {
		t.Errorf("VCS = %q, want empty", r.VCS)
	}
}
