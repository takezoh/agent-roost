package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Tmux.SessionName != "roost" {
		t.Errorf("SessionName = %q, want %q", cfg.Tmux.SessionName, "roost")
	}
	if cfg.Monitor.PollIntervalMs != 1000 {
		t.Errorf("PollIntervalMs = %d, want 1000", cfg.Monitor.PollIntervalMs)
	}
	if cfg.Session.DefaultCommand != "claude" {
		t.Errorf("DefaultCommand = %q, want %q", cfg.Session.DefaultCommand, "claude")
	}
	if len(cfg.Session.Commands) != 3 {
		t.Errorf("len(Commands) = %d, want 3", len(cfg.Session.Commands))
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := ExpandPath("~/foo")
	want := filepath.Join(home, "foo")
	if got != want {
		t.Errorf("ExpandPath(~/foo) = %q, want %q", got, want)
	}
	if got := ExpandPath("/abs/path"); got != "/abs/path" {
		t.Errorf("ExpandPath(/abs/path) = %q, want /abs/path", got)
	}
}

func TestListProjects(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "proj-a"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "proj-b"), 0o755)
	os.MkdirAll(filepath.Join(tmp, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(tmp, "README"), []byte("hi"), 0o644)

	cfg := &Config{Projects: ProjectsConfig{ProjectRoots: []string{tmp}}}
	projects := cfg.ListProjects()
	if len(projects) != 2 {
		t.Fatalf("len(projects) = %d, want 2; got %v", len(projects), projects)
	}
	names := map[string]bool{}
	for _, p := range projects {
		names[filepath.Base(p)] = true
	}
	if !names["proj-a"] || !names["proj-b"] {
		t.Errorf("expected proj-a and proj-b, got %v", projects)
	}
}

func TestResolveDataDir_Explicit(t *testing.T) {
	cfg := &Config{DataDir: "/tmp/data"}
	if got := cfg.ResolveDataDir(); got != "/tmp/data" {
		t.Errorf("ResolveDataDir() = %q, want /tmp/data", got)
	}
}

func TestResolveDataDir_Fallback(t *testing.T) {
	cfg := &Config{}
	want := ConfigDir()
	if got := cfg.ResolveDataDir(); got != want {
		t.Errorf("ResolveDataDir() = %q, want %q", got, want)
	}
}
