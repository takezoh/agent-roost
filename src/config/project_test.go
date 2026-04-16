package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeProjectSettings creates <dir>/.roost/settings.toml with the given
// content and returns the path.
func writeProjectSettings(t *testing.T, dir, content string) string {
	t.Helper()
	roostDir := filepath.Join(dir, ".roost")
	if err := os.MkdirAll(roostDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(roostDir, "settings.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- LoadProjectFrom ---

func TestLoadProjectFrom_Missing(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadProjectFrom(filepath.Join(dir, "nonexistent.toml"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkspaceName() != DefaultWorkspaceName {
		t.Errorf("WorkspaceName = %q, want %q", cfg.WorkspaceName(), DefaultWorkspaceName)
	}
}

func TestLoadProjectFrom_Valid(t *testing.T) {
	dir := t.TempDir()
	path := writeProjectSettings(t, dir, "[workspace]\nname = \"work\"\n")
	cfg, err := LoadProjectFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkspaceName() != "work" {
		t.Errorf("WorkspaceName = %q, want %q", cfg.WorkspaceName(), "work")
	}
}

func TestLoadProjectFrom_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	path := filepath.Join(roostDir, "settings.toml")
	os.WriteFile(path, []byte("[[[[bad toml"), 0o644)
	_, err := LoadProjectFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML, got nil")
	}
}

// --- LoadProject ---

func TestLoadProject_FindsInProjectDir(t *testing.T) {
	dir := t.TempDir()
	writeProjectSettings(t, dir, "[workspace]\nname = \"oss\"\n")
	cfg, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkspaceName() != "oss" {
		t.Errorf("WorkspaceName = %q, want %q", cfg.WorkspaceName(), "oss")
	}
}

func TestLoadProject_FindsAncestor(t *testing.T) {
	root := t.TempDir()
	writeProjectSettings(t, root, "[workspace]\nname = \"parent-ws\"\n")
	sub := filepath.Join(root, "src", "pkg")
	os.MkdirAll(sub, 0o755)
	cfg, err := LoadProject(sub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkspaceName() != "parent-ws" {
		t.Errorf("WorkspaceName = %q, want %q", cfg.WorkspaceName(), "parent-ws")
	}
}

func TestLoadProject_NoSettingsReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WorkspaceName() != DefaultWorkspaceName {
		t.Errorf("WorkspaceName = %q, want %q", cfg.WorkspaceName(), DefaultWorkspaceName)
	}
}

// --- WorkspaceName ---

func TestWorkspaceName_DefaultsWhenEmpty(t *testing.T) {
	cfg := &ProjectConfig{}
	if got := cfg.WorkspaceName(); got != DefaultWorkspaceName {
		t.Errorf("WorkspaceName = %q, want %q", got, DefaultWorkspaceName)
	}
}

func TestWorkspaceName_TrimsWhitespace(t *testing.T) {
	cfg := &ProjectConfig{Workspace: ProjectWorkspaceConfig{Name: "  work  "}}
	if got := cfg.WorkspaceName(); got != "work" {
		t.Errorf("WorkspaceName = %q, want %q", got, "work")
	}
}

// --- Validate ---

func TestValidate_OK(t *testing.T) {
	cfg := &ProjectConfig{Workspace: ProjectWorkspaceConfig{Name: "work"}}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_EmptyNameOK(t *testing.T) {
	cfg := &ProjectConfig{}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error for empty name: %v", err)
	}
}

func TestValidate_RejectsWhitespaceOnly(t *testing.T) {
	cfg := &ProjectConfig{Workspace: ProjectWorkspaceConfig{Name: "   "}}
	// whitespace-only trims to "" → valid (treated as default)
	if err := cfg.Validate(); err != nil {
		t.Errorf("whitespace-only name should be valid (treated as default): %v", err)
	}
	if cfg.WorkspaceName() != DefaultWorkspaceName {
		t.Errorf("WorkspaceName should fall back to default for whitespace-only name")
	}
}

func TestValidate_RejectsTooLong(t *testing.T) {
	cfg := &ProjectConfig{Workspace: ProjectWorkspaceConfig{Name: strings.Repeat("a", MaxWorkspaceNameLen+1)}}
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for too-long name, got nil")
	}
}

func TestValidate_RejectsControlCharacter(t *testing.T) {
	cfg := &ProjectConfig{Workspace: ProjectWorkspaceConfig{Name: "bad\x01name"}}
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for control character, got nil")
	}
}

// --- WorkspaceResolver ---

func TestWorkspaceResolver_CachesByMtime(t *testing.T) {
	dir := t.TempDir()
	writeProjectSettings(t, dir, "[workspace]\nname = \"cached\"\n")

	r := NewWorkspaceResolver()
	ws1 := r.Resolve(dir)
	ws2 := r.Resolve(dir) // should be served from cache
	if ws1 != "cached" || ws2 != "cached" {
		t.Errorf("Resolve = %q, %q; want %q, %q", ws1, ws2, "cached", "cached")
	}
}

func TestWorkspaceResolver_ReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	path := writeProjectSettings(t, dir, "[workspace]\nname = \"v1\"\n")

	r := NewWorkspaceResolver()
	if got := r.Resolve(dir); got != "v1" {
		t.Fatalf("first resolve = %q, want v1", got)
	}

	// Overwrite with different content and bump mtime by at least 1ms.
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(path, []byte("[workspace]\nname = \"v2\"\n"), 0o644)

	if got := r.Resolve(dir); got != "v2" {
		t.Errorf("after file change, resolve = %q, want v2", got)
	}
}

func TestWorkspaceResolver_HandlesInvalidTomlAsDefault(t *testing.T) {
	dir := t.TempDir()
	roostDir := filepath.Join(dir, ".roost")
	os.MkdirAll(roostDir, 0o755)
	os.WriteFile(filepath.Join(roostDir, "settings.toml"), []byte("[[[[bad"), 0o644)

	r := NewWorkspaceResolver()
	if got := r.Resolve(dir); got != DefaultWorkspaceName {
		t.Errorf("invalid TOML should fall back to default, got %q", got)
	}
}

func TestWorkspaceResolver_NoSettings(t *testing.T) {
	dir := t.TempDir()
	r := NewWorkspaceResolver()
	if got := r.Resolve(dir); got != DefaultWorkspaceName {
		t.Errorf("no settings should return default, got %q", got)
	}
}

func TestWorkspaceResolver_Invalidate(t *testing.T) {
	dir := t.TempDir()
	writeProjectSettings(t, dir, "[workspace]\nname = \"a\"\n")
	r := NewWorkspaceResolver()
	r.Resolve(dir)

	r.Invalidate()
	// After invalidation the entry should be re-read (same result, different code path).
	if got := r.Resolve(dir); got != "a" {
		t.Errorf("after Invalidate, resolve = %q, want a", got)
	}
}
