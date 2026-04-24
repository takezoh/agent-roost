package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxResolver_EmptyProject(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	r := NewSandboxResolver(user)
	got := r.Resolve("")
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (empty project returns user config)", got.Mode)
	}
}

func TestSandboxResolver_NoSettingsFile(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	r := NewSandboxResolver(user)
	got := r.Resolve(t.TempDir()) // no .roost/settings.toml
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (absent settings returns user config)", got.Mode)
	}
}

func TestSandboxResolver_ProjectOverridesMode(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	os.WriteFile(filepath.Join(roostDir, "settings.toml"), []byte(`[sandbox]
mode = "direct"
`), 0o644)

	r := NewSandboxResolver(user)
	got := r.Resolve(dir)
	if got.Mode != "direct" {
		t.Errorf("Mode = %q, want direct (project overrides)", got.Mode)
	}
}

func TestSandboxResolver_ProjectNoSandboxSection(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	os.WriteFile(filepath.Join(roostDir, "settings.toml"), []byte(`[workspace]
name = "myproject"
`), 0o644)

	r := NewSandboxResolver(user)
	got := r.Resolve(dir)
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (no sandbox section → user config)", got.Mode)
	}
}

func TestSandboxResolver_CacheHit(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	path := filepath.Join(roostDir, "settings.toml")
	os.WriteFile(path, []byte(`[sandbox]
mode = "direct"
`), 0o644)

	r := NewSandboxResolver(user)
	got1 := r.Resolve(dir)
	// overwrite file without changing its content — mtime-based cache should hit
	// (we can't easily test mtime hit without sleeping, so just confirm consistency)
	got2 := r.Resolve(dir)
	if got1.Mode != got2.Mode {
		t.Errorf("inconsistent results: %q vs %q", got1.Mode, got2.Mode)
	}
}

func TestSandboxResolver_ParseError_FallsBackToUser(t *testing.T) {
	user := SandboxConfig{Mode: "docker"}
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	os.WriteFile(filepath.Join(roostDir, "settings.toml"), []byte("invalid toml :::"), 0o644)

	r := NewSandboxResolver(user)
	got := r.Resolve(dir)
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (parse error falls back to user)", got.Mode)
	}
}

func TestSandboxResolver_DockerImageOverride(t *testing.T) {
	user := SandboxConfig{Mode: "docker", Docker: DockerConfig{Image: "node:22"}}
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	os.WriteFile(filepath.Join(roostDir, "settings.toml"), []byte(`[sandbox.docker]
image = "custom:latest"
`), 0o644)

	r := NewSandboxResolver(user)
	got := r.Resolve(dir)
	if got.Docker.Image != "custom:latest" {
		t.Errorf("Image = %q, want custom:latest (project overrides)", got.Docker.Image)
	}
	if got.Mode != "docker" {
		t.Errorf("Mode = %q, want docker (not overridden)", got.Mode)
	}
}
