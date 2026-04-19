package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
