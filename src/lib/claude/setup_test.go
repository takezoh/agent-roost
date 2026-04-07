package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterHooks_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	events, err := RegisterHooks(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected registered events, got none")
	}
	if events[0] != "SessionStart" {
		t.Errorf("events[0] = %q, want %q", events[0], "SessionStart")
	}

	data, _ := os.ReadFile(path)
	var settings map[string]any
	json.Unmarshal(data, &settings)

	hooks := settings["hooks"].(map[string]any)
	entries := hooks["SessionStart"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}

	entry := entries[0].(map[string]any)
	hookArr := entry["hooks"].([]any)
	hook := hookArr[0].(map[string]any)
	if hook["command"] != "/usr/local/bin/roost claude event" {
		t.Errorf("command = %v", hook["command"])
	}
}

func TestRegisterHooks_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"permissions":{"allow":["Read"]}}`), 0o644)

	events, err := RegisterHooks(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected registered events, got none")
	}

	data, _ := os.ReadFile(path)
	var settings map[string]any
	json.Unmarshal(data, &settings)

	if _, ok := settings["permissions"]; !ok {
		t.Error("existing permissions field was removed")
	}
}

func TestRegisterHooks_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	RegisterHooks(path, "/usr/local/bin/roost")
	events, err := RegisterHooks(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil for already registered, got %v", events)
	}
}

func TestUnregisterHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	RegisterHooks(path, "/usr/local/bin/roost")
	if err := UnregisterHooks(path); err != nil {
		t.Fatalf("UnregisterHooks: %v", err)
	}

	registered, _ := IsHookRegistered(path)
	if registered {
		t.Error("hooks still registered after unregister")
	}
}

func TestIsHookRegistered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	registered, _ := IsHookRegistered(path)
	if registered {
		t.Error("expected false for non-existent file")
	}

	RegisterHooks(path, "/usr/local/bin/roost")
	registered, _ = IsHookRegistered(path)
	if !registered {
		t.Error("expected true after registration")
	}
}
