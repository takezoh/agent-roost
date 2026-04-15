package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
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

	got := DetectRemoteHost(context.Background(), dir)
	if got != "github.com" {
		t.Errorf("DetectRemoteHost = %q, want %q", got, "github.com")
	}
}

func TestDetectRemoteHost_HTTPS(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "remote", "add", "origin", "https://gitlab.com/user/repo.git")

	got := DetectRemoteHost(context.Background(), dir)
	if got != "gitlab.com" {
		t.Errorf("DetectRemoteHost = %q, want %q", got, "gitlab.com")
	}
}

func TestDetectRemoteHost_NoRemote(t *testing.T) {
	dir := initRepo(t)

	got := DetectRemoteHost(context.Background(), dir)
	if got != "" {
		t.Errorf("DetectRemoteHost = %q, want empty", got)
	}
}

func TestDetectRemoteHost_NotGitDir(t *testing.T) {
	dir := t.TempDir()

	got := DetectRemoteHost(context.Background(), dir)
	if got != "" {
		t.Errorf("DetectRemoteHost = %q, want empty", got)
	}
}

func TestIsWorktree_MainRepo(t *testing.T) {
	dir := initRepo(t)
	if IsWorktree(dir) {
		t.Error("IsWorktree = true for main repo, want false")
	}
}

func TestIsWorktree_LinkedWorktree(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "branch", "feature")
	wtDir := t.TempDir()
	gitRun(t, dir, "worktree", "add", wtDir, "feature")

	if !IsWorktree(wtDir) {
		t.Error("IsWorktree = false for linked worktree, want true")
	}
}

func TestDetectMainBranch_LinkedWorktree(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "branch", "feature")
	wtDir := t.TempDir()
	gitRun(t, dir, "worktree", "add", wtDir, "feature")

	got := DetectMainBranch(context.Background(), wtDir)
	if got != "main" {
		t.Errorf("DetectMainBranch = %q, want %q", got, "main")
	}
}

func TestDetectMainBranch_NoGit(t *testing.T) {
	dir := t.TempDir()
	got := DetectMainBranch(context.Background(), dir)
	if got != "" {
		t.Errorf("DetectMainBranch = %q, want empty", got)
	}
}

func TestParseMainBranch(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "normal",
			output: "worktree /home/user/repo\nHEAD abc123\nbranch refs/heads/main\n\nworktree /tmp/wt\nHEAD def456\nbranch refs/heads/feature\n\n",
			want:   "main",
		},
		{
			name:   "detached HEAD in main",
			output: "worktree /home/user/repo\nHEAD abc123\ndetached\n\nworktree /tmp/wt\nHEAD def456\nbranch refs/heads/feature\n\n",
			want:   "",
		},
		{
			name:   "empty",
			output: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseMainBranch(tt.output); got != tt.want {
				t.Errorf("parseMainBranch = %q, want %q", got, tt.want)
			}
		})
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

func TestRepoRoot(t *testing.T) {
	dir := initRepo(t)
	root, err := RepoRoot(context.Background(), dir)
	if err != nil {
		t.Fatalf("RepoRoot error: %v", err)
	}
	if root != dir {
		t.Fatalf("RepoRoot = %q, want %q", root, dir)
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()
	wtDir, err := CreateWorktree(ctx, dir, "feature-test")
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}
	want := filepath.Join(dir, ".roost", "worktrees", "feature-test")
	if wtDir != want {
		t.Fatalf("CreateWorktree path = %q, want %q", wtDir, want)
	}
	if !IsWorktree(wtDir) {
		t.Fatal("created path is not a linked worktree")
	}
	if got := DetectBranch(ctx, wtDir); got != "feature-test" {
		t.Fatalf("branch = %q, want %q", got, "feature-test")
	}
}

func TestCreateWorktreeRejectsNonGitDir(t *testing.T) {
	if _, err := CreateWorktree(context.Background(), t.TempDir(), "feature-test"); err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestRemoveWorktree(t *testing.T) {
	dir := initRepo(t)
	ctx := context.Background()
	wtDir, err := CreateWorktree(ctx, dir, "feature-test")
	if err != nil {
		t.Fatalf("CreateWorktree error: %v", err)
	}
	if err := RemoveWorktree(ctx, wtDir); err != nil {
		t.Fatalf("RemoveWorktree error: %v", err)
	}
	if _, err := os.Stat(wtDir); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists after remove: %v", err)
	}
}

func TestRemoveWorktreeRejectsUnmanagedPath(t *testing.T) {
	dir := initRepo(t)
	if err := RemoveWorktree(context.Background(), dir); err == nil {
		t.Fatal("expected error for unmanaged path")
	}
}

func TestFindGitRoot(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "sub")
	subsub := filepath.Join(sub, "subsub")
	if err := os.MkdirAll(subsub, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		dir  string
		want string
	}{
		{"repo root itself", dir, dir},
		{"direct child", sub, dir},
		{"grandchild", subsub, dir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findGitRoot(tt.dir); got != tt.want {
				t.Errorf("findGitRoot(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}

func TestDetectBranch_FromSubdirectory(t *testing.T) {
	dir := initRepo(t)
	sub := filepath.Join(dir, "src", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := DetectBranch(context.Background(), sub); got != "main" {
		t.Errorf("DetectBranch(subdir) = %q, want %q", got, "main")
	}
}

func TestIsWorktree_SubdirectoryOfLinkedWorktree(t *testing.T) {
	dir := initRepo(t)
	gitRun(t, dir, "branch", "feature")
	wtDir := t.TempDir()
	gitRun(t, dir, "worktree", "add", wtDir, "feature")
	sub := filepath.Join(wtDir, "deep", "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if !IsWorktree(sub) {
		t.Error("IsWorktree(subdir-of-worktree) = false, want true")
	}
}
