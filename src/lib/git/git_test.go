package git

import (
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

func initRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@test.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "init")
	return dir
}

func TestDetectRemoteHost_SSH(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "git@github.com:user/repo.git")

	got := DetectRemoteHost(dir)
	if got != "github.com" {
		t.Errorf("DetectRemoteHost = %q, want %q", got, "github.com")
	}
}

func TestDetectRemoteHost_HTTPS(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://gitlab.com/user/repo.git")

	got := DetectRemoteHost(dir)
	if got != "gitlab.com" {
		t.Errorf("DetectRemoteHost = %q, want %q", got, "gitlab.com")
	}
}

func TestDetectRemoteHost_NoRemote(t *testing.T) {
	dir := initRepo(t)

	got := DetectRemoteHost(dir)
	if got != "" {
		t.Errorf("DetectRemoteHost = %q, want empty", got)
	}
}

func TestDetectRemoteHost_NotGitDir(t *testing.T) {
	dir := t.TempDir()

	got := DetectRemoteHost(dir)
	if got != "" {
		t.Errorf("DetectRemoteHost = %q, want empty", got)
	}
}

func TestParseHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"git@github.com:user/repo.git", "github.com"},
		{"ssh://git@github.com/user/repo.git", "github.com"},
		{"https://gitlab.com/user/repo.git", "gitlab.com"},
		{"https://bitbucket.org/user/repo.git", "bitbucket.org"},
		{"git@GitHub.COM:user/repo.git", "github.com"},
		{"/local/path/repo.git", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := parseHost(tt.raw); got != tt.want {
			t.Errorf("parseHost(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}
