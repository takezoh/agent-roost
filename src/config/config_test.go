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
	if cfg.Session.DefaultCommand != "shell" {
		t.Errorf("DefaultCommand = %q, want %q", cfg.Session.DefaultCommand, "shell")
	}
	if len(cfg.Session.Commands) != 1 || cfg.Session.Commands[0] != "shell" {
		t.Errorf("Commands = %v, want [shell]", cfg.Session.Commands)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if len(cfg.Session.PushCommands) != 1 || cfg.Session.PushCommands[0] != "shell" {
		t.Errorf("PushCommands = %v, want [shell]", cfg.Session.PushCommands)
	}
}

func TestLoadFrom_PushCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[session]
push_commands = ["shell", "claude"]
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Session.PushCommands) != 2 {
		t.Fatalf("PushCommands = %v, want [shell, claude]", cfg.Session.PushCommands)
	}
	if cfg.Session.PushCommands[0] != "shell" || cfg.Session.PushCommands[1] != "claude" {
		t.Errorf("PushCommands = %v, want [shell, claude]", cfg.Session.PushCommands)
	}
}

func TestLoadFrom_LogLevel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[log]
level = "debug"
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "debug")
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

func TestListProjects_WithProjectPaths(t *testing.T) {
	tmp := t.TempDir()
	direct := filepath.Join(tmp, "direct-proj")
	os.MkdirAll(direct, 0o755)
	nonexistent := filepath.Join(tmp, "does-not-exist")

	cfg := &Config{Projects: ProjectsConfig{ProjectPaths: []string{direct, nonexistent}}}
	projects := cfg.ListProjects()
	if len(projects) != 1 {
		t.Fatalf("len(projects) = %d, want 1; got %v", len(projects), projects)
	}
	if projects[0] != direct {
		t.Errorf("projects[0] = %q, want %q", projects[0], direct)
	}
}

func TestListProjects_RootsAndPaths(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, "roots", "proj-a"), 0o755)
	direct := filepath.Join(tmp, "direct-proj")
	os.MkdirAll(direct, 0o755)

	cfg := &Config{Projects: ProjectsConfig{
		ProjectRoots: []string{filepath.Join(tmp, "roots")},
		ProjectPaths: []string{direct},
	}}
	projects := cfg.ListProjects()
	if len(projects) != 2 {
		t.Fatalf("len(projects) = %d, want 2; got %v", len(projects), projects)
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
	want := ConfigDirPath()
	if got := cfg.ResolveDataDir(); got != want {
		t.Errorf("ResolveDataDir() = %q, want %q", got, want)
	}
}

func TestLoadFrom_Missing(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/settings.toml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tmux.SessionName != "roost" {
		t.Fatalf("expected defaults, got session_name=%s", cfg.Tmux.SessionName)
	}
}

func TestLoadFrom_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[tmux]
session_name = "custom"
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tmux.SessionName != "custom" {
		t.Fatalf("expected custom, got %s", cfg.Tmux.SessionName)
	}
	if cfg.Monitor.PollIntervalMs != 1000 {
		t.Fatalf("expected default 1000, got %d", cfg.Monitor.PollIntervalMs)
	}
}

func TestSessionAliases_LoadAndResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[session.aliases]
clw = "claude --worktree"
cw = "codex --workspace"
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Session.Aliases["clw"]; got != "claude --worktree" {
		t.Errorf("Aliases[clw] = %q, want %q", got, "claude --worktree")
	}
	if got := cfg.Session.ResolveAlias("clw"); got != "claude --worktree" {
		t.Errorf("ResolveAlias(clw) = %q, want %q", got, "claude --worktree")
	}
	if got := cfg.Session.ResolveAlias("  clw  "); got != "claude --worktree" {
		t.Errorf("ResolveAlias trims whitespace, got %q", got)
	}
	if got := cfg.Session.ResolveAlias("claude"); got != "claude" {
		t.Errorf("unknown alias should pass through, got %q", got)
	}
}

func TestLoadFrom_DriversSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[drivers.claude]
show_thinking = true
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	claude, ok := cfg.Drivers["claude"]
	if !ok {
		t.Fatal("expected drivers.claude section")
	}
	if claude["show_thinking"] != true {
		t.Errorf("show_thinking = %v, want true", claude["show_thinking"])
	}
}

func TestLoadFrom_FeaturesEnabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[features.enabled]
example-feature = true
another = false
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Features.Enabled["example-feature"] != true {
		t.Errorf("features.enabled[example-feature] = %v, want true", cfg.Features.Enabled["example-feature"])
	}
	if cfg.Features.Enabled["another"] != false {
		t.Errorf("features.enabled[another] = %v, want false", cfg.Features.Enabled["another"])
	}
}

func TestLoadFrom_FeaturesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[tmux]
session_name = "test"
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Features.Enabled) != 0 {
		t.Errorf("expected empty Features.Enabled, got %v", cfg.Features.Enabled)
	}
}

func TestLoadFrom_DriversEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[tmux]
session_name = "test"
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Drivers) != 0 {
		t.Errorf("expected empty Drivers, got %v", cfg.Drivers)
	}
}

func TestResolveDocker_NoDriverOverride(t *testing.T) {
	s := SandboxConfig{
		Docker: DockerConfig{
			Image:      "node:22",
			Network:    "roost-sandbox",
			ForwardEnv: []string{"ANTHROPIC_API_KEY"},
		},
	}
	got := s.ResolveDocker("shell")
	if got.Image != "node:22" {
		t.Errorf("Image = %q, want node:22", got.Image)
	}
	if len(got.ForwardEnv) != 1 || got.ForwardEnv[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("ForwardEnv = %v, want [ANTHROPIC_API_KEY]", got.ForwardEnv)
	}
}

func TestResolveDocker_ScalarOverride(t *testing.T) {
	s := SandboxConfig{
		Docker: DockerConfig{
			Image: "base:latest",
			Drivers: map[string]DriverDockerConfig{
				"claude": {Image: "claude:latest"},
			},
		},
	}
	got := s.ResolveDocker("claude")
	if got.Image != "claude:latest" {
		t.Errorf("Image = %q, want claude:latest (driver wins)", got.Image)
	}
	// empty driver image → base wins
	got2 := s.ResolveDocker("shell")
	if got2.Image != "base:latest" {
		t.Errorf("Image = %q, want base:latest (no driver override)", got2.Image)
	}
}

func TestResolveDocker_ListAdditive(t *testing.T) {
	s := SandboxConfig{
		Docker: DockerConfig{
			ExtraArgs:   []string{"--read-only"},
			ExtraMounts: []string{"/a:/a:ro"},
			ForwardEnv:  []string{"ANTHROPIC_API_KEY"},
			Drivers: map[string]DriverDockerConfig{
				"claude": {
					ExtraArgs:   []string{"--shm-size=256m"},
					ExtraMounts: []string{"~/.claude:/home/user/.claude:rw"},
					ForwardEnv:  []string{"CLAUDE_CODE_CONFIG_DIR"},
				},
			},
		},
	}
	got := s.ResolveDocker("claude")
	if len(got.ExtraArgs) != 2 {
		t.Errorf("ExtraArgs len = %d, want 2 (base+driver concat)", len(got.ExtraArgs))
	}
	if len(got.ExtraMounts) != 2 {
		t.Errorf("ExtraMounts len = %d, want 2 (base+driver concat)", len(got.ExtraMounts))
	}
	if len(got.ForwardEnv) != 2 {
		t.Errorf("ForwardEnv len = %d, want 2 (base+driver concat)", len(got.ForwardEnv))
	}
}

func TestResolveDocker_MapMerge(t *testing.T) {
	s := SandboxConfig{
		Docker: DockerConfig{
			Env: map[string]string{"HOME": "/home/user", "FOO": "base"},
			Drivers: map[string]DriverDockerConfig{
				"claude": {Env: map[string]string{"FOO": "driver", "BAR": "added"}},
			},
		},
	}
	got := s.ResolveDocker("claude")
	if got.Env["HOME"] != "/home/user" {
		t.Errorf("Env[HOME] = %q, want /home/user (base survives)", got.Env["HOME"])
	}
	if got.Env["FOO"] != "driver" {
		t.Errorf("Env[FOO] = %q, want driver (driver wins)", got.Env["FOO"])
	}
	if got.Env["BAR"] != "added" {
		t.Errorf("Env[BAR] = %q, want added (driver adds new key)", got.Env["BAR"])
	}
}

func TestLoadFrom_SandboxDockerDrivers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`[sandbox]
mode = "docker"

[sandbox.docker]
image = "node:22-bookworm-slim"
forward_env = ["ANTHROPIC_API_KEY"]

[sandbox.docker.driver.claude]
image = "ghcr.io/anthropics/claude-code:latest"
extra_mounts = ["~/.claude:/home/user/.claude:rw"]
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox.Mode != "docker" {
		t.Errorf("Mode = %q, want docker", cfg.Sandbox.Mode)
	}
	if cfg.Sandbox.Docker.Image != "node:22-bookworm-slim" {
		t.Errorf("base Image = %q", cfg.Sandbox.Docker.Image)
	}
	got := cfg.Sandbox.ResolveDocker("claude")
	if got.Image != "ghcr.io/anthropics/claude-code:latest" {
		t.Errorf("claude Image = %q", got.Image)
	}
	if len(got.ExtraMounts) != 1 {
		t.Errorf("ExtraMounts len = %d, want 1", len(got.ExtraMounts))
	}
	if len(got.ForwardEnv) != 1 || got.ForwardEnv[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("ForwardEnv = %v", got.ForwardEnv)
	}
}
