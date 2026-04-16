package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureNoHooksSettingsCreatesFile(t *testing.T) {
	dataDir := t.TempDir()
	kv, err := EnsureNoHooksSettings(dataDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(kv, "GEMINI_CLI_SYSTEM_SETTINGS_PATH=") {
		t.Errorf("unexpected env entry: %q", kv)
	}
	path := strings.TrimPrefix(kv, "GEMINI_CLI_SYSTEM_SETTINGS_PATH=")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shadow file not created at %q: %v", path, err)
	}

	data, _ := os.ReadFile(path)
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("shadow file is not valid JSON: %v", err)
	}
	hc, _ := settings["hooksConfig"].(map[string]any)
	if enabled, _ := hc["enabled"].(bool); enabled {
		t.Errorf("expected hooksConfig.enabled=false, got true")
	}
}

func TestEnsureNoHooksSettingsIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(dataDir, "scripts", "gemini-no-hooks.json")

	kv1, err := EnsureNoHooksSettings(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	info1, _ := os.Stat(path)

	kv2, err := EnsureNoHooksSettings(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(path)

	if kv1 != kv2 {
		t.Errorf("env entry changed between calls: %q vs %q", kv1, kv2)
	}
	if info1.ModTime() != info2.ModTime() {
		t.Errorf("shadow file was rewritten on second call (mtime changed)")
	}
}
