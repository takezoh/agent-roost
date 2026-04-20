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
	if hook["command"] != "/usr/local/bin/roost event claude" {
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

func TestRegisterMCPServer_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	added, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	if !added {
		t.Error("expected added=true for new file")
	}

	data, _ := os.ReadFile(path)
	var settings map[string]any
	json.Unmarshal(data, &settings)

	mcpServers, ok := settings["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("mcpServers key missing or wrong type")
	}
	entry, ok := mcpServers["roost-peers"].(map[string]any)
	if !ok {
		t.Fatal("roost-peers entry missing")
	}
	if entry["command"] != "/usr/local/bin/roost" {
		t.Errorf("command = %v, want /usr/local/bin/roost", entry["command"])
	}
	args, _ := entry["args"].([]any)
	if len(args) == 0 || args[0] != "peers-mcp" {
		t.Errorf("args = %v, want [peers-mcp]", args)
	}
}

func TestRegisterMCPServer_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	added1, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("first RegisterMCPServer: %v", err)
	}
	if !added1 {
		t.Error("expected added=true on first call")
	}

	added2, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("second RegisterMCPServer: %v", err)
	}
	if added2 {
		t.Error("expected added=false on second call (already registered)")
	}
}

func TestRegisterMCPServer_PreservesExistingKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"permissions":{"allow":["Read"]}}`), 0o644)

	_, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	data, _ := os.ReadFile(path)
	var settings map[string]any
	json.Unmarshal(data, &settings)

	if _, ok := settings["permissions"]; !ok {
		t.Error("existing permissions field was removed")
	}
	if _, ok := settings["mcpServers"]; !ok {
		t.Error("mcpServers was not written")
	}
}
