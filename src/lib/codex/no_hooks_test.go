package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureNoHooksHomeCreatesFiles(t *testing.T) {
	dataDir := t.TempDir()
	kv, err := EnsureNoHooksHome(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(kv, "CODEX_HOME=") {
		t.Errorf("unexpected env entry: %q", kv)
	}
	shadowDir := strings.TrimPrefix(kv, "CODEX_HOME=")

	configPath := filepath.Join(shadowDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	if !strings.Contains(string(data), "codex_hooks = false") {
		t.Errorf("config.toml does not contain codex_hooks = false: %s", data)
	}

	hooksPath := filepath.Join(shadowDir, "hooks.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("hooks.json not created: %v", err)
	}
}

func TestEnsureNoHooksHomeIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	shadowDir := filepath.Join(dataDir, "scripts", "codex-no-hooks")

	kv1, err := EnsureNoHooksHome(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(filepath.Join(shadowDir, "config.toml"))

	kv2, err := EnsureNoHooksHome(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(filepath.Join(shadowDir, "config.toml"))

	if kv1 != kv2 {
		t.Errorf("env entry changed between calls: %q vs %q", kv1, kv2)
	}
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("config.toml was rewritten on second call (mtime changed)")
	}
}
