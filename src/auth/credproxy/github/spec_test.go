package github

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takezoh/agent-roost/config"
)

func stubGh(t *testing.T, token string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "gh")
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gh: %v", err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func ghConfig(enabled bool) config.SandboxConfig {
	return config.SandboxConfig{
		Proxy: config.ProxyConfig{
			GitHub: config.GitHubConfig{Enabled: enabled},
		},
	}
}

func TestSpecBuilder_disabled(t *testing.T) {
	b := NewSpecBuilder("127.0.0.1:9999", "tok", t.TempDir())
	spec, err := b.ContainerSpec(context.Background(), "/proj", ghConfig(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec when disabled, got %+v", spec)
	}
}

func TestSpecBuilder_enabled_gh_missing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // gh not in PATH
	b := NewSpecBuilder("127.0.0.1:9999", "tok", t.TempDir())
	spec, err := b.ContainerSpec(context.Background(), "/proj", ghConfig(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 {
		t.Errorf("expected zero spec when gh missing, got %+v", spec)
	}
}

func TestSpecBuilder_enabled_gh_present(t *testing.T) {
	stubGh(t, "ghp_testtoken")
	gitDir := t.TempDir()
	b := NewSpecBuilder("127.0.0.1:9999", "mytoken", gitDir)
	if err := b.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	spec, err := b.ContainerSpec(context.Background(), "/proj", ghConfig(true))
	if err != nil {
		t.Fatal(err)
	}

	if spec.Env["GIT_CONFIG_GLOBAL"] != containerGitconfigPath {
		t.Errorf("GIT_CONFIG_GLOBAL = %q, want %q", spec.Env["GIT_CONFIG_GLOBAL"], containerGitconfigPath)
	}
	if spec.Env["ROOST_GIT_TOKEN"] != "mytoken" {
		t.Errorf("ROOST_GIT_TOKEN = %q, want %q", spec.Env["ROOST_GIT_TOKEN"], "mytoken")
	}
	if spec.Env["ROOST_PROXY_PORT"] != "9999" {
		t.Errorf("ROOST_PROXY_PORT = %q, want 9999", spec.Env["ROOST_PROXY_PORT"])
	}
	if len(spec.Mounts) != 2 {
		t.Errorf("expected 2 mounts, got %v", spec.Mounts)
	}
}

func TestSpecBuilder_init_creates_files(t *testing.T) {
	gitDir := t.TempDir()
	b := NewSpecBuilder("127.0.0.1:9999", "tok", gitDir)
	if err := b.Init(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"git-credential-roost", "gitconfig"} {
		path := filepath.Join(gitDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected %s to exist: %v", name, err)
		}
	}

	// helper script must be executable
	info, _ := os.Stat(filepath.Join(gitDir, "git-credential-roost"))
	if info.Mode()&0o111 == 0 {
		t.Error("git-credential-roost is not executable")
	}
}
