package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCodexHooksEnabled_AppendsSection(t *testing.T) {
	out, changed := ensureCodexHooksEnabled("model = \"gpt-5-codex\"\n")
	if !changed {
		t.Fatal("expected changed")
	}
	if !strings.Contains(out, "[features]") {
		t.Fatal("missing [features] section")
	}
	if !strings.Contains(out, "codex_hooks = true") {
		t.Fatal("missing codex_hooks setting")
	}
}

func TestEnsureCodexHooksEnabled_InSection(t *testing.T) {
	in := "[features]\nverbose = true\n[profiles.default]\nmodel = \"x\"\n"
	out, changed := ensureCodexHooksEnabled(in)
	if !changed {
		t.Fatal("expected changed")
	}
	want := "[features]\nverbose = true\ncodex_hooks = true\n"
	if !strings.Contains(out, want) {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestEnsureCodexHooksEnabled_Idempotent(t *testing.T) {
	in := "[features]\ncodex_hooks = true\n"
	out, changed := ensureCodexHooksEnabled(in)
	if changed {
		t.Fatal("expected unchanged")
	}
	if out != in {
		t.Fatalf("output changed:\n%s", out)
	}
}

func TestRegisterHooks_WritesFilesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	hooksPath := filepath.Join(dir, "hooks.json")
	roost := "/usr/local/bin/roost"

	changed, events, err := RegisterHooks(cfgPath, hooksPath, roost)
	if err != nil {
		t.Fatalf("RegisterHooks error: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if len(events) == 0 {
		t.Fatal("expected added events")
	}

	cfg, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(cfg), "codex_hooks = true") {
		t.Fatalf("config missing codex_hooks:\n%s", string(cfg))
	}

	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	var hf hooksFile
	if err := json.Unmarshal(raw, &hf); err != nil {
		t.Fatalf("unmarshal hooks: %v", err)
	}
	if len(hf.Hooks["SessionStart"]) == 0 {
		t.Fatal("missing SessionStart hook")
	}

	changed2, events2, err := RegisterHooks(cfgPath, hooksPath, roost)
	if err != nil {
		t.Fatalf("RegisterHooks second run error: %v", err)
	}
	if changed2 {
		t.Fatal("second run should be unchanged")
	}
	if len(events2) != 0 {
		t.Fatalf("second run events = %v, want empty", events2)
	}
}

func TestRegisterHooks_PreservesExistingData(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	hooksPath := filepath.Join(dir, "hooks.json")

	err := os.WriteFile(cfgPath, []byte("theme = \"dark\"\n"), 0o644)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	err = os.WriteFile(hooksPath, []byte(`{"hooks":{"Custom":[{"hooks":[{"type":"command","command":"echo hi"}]}]}}`), 0o644)
	if err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	_, _, err = RegisterHooks(cfgPath, hooksPath, "/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}

	cfg, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(cfg), "theme = \"dark\"") {
		t.Fatalf("existing key lost:\n%s", string(cfg))
	}

	raw, _ := os.ReadFile(hooksPath)
	var hf hooksFile
	if err := json.Unmarshal(raw, &hf); err != nil {
		t.Fatalf("unmarshal hooks: %v", err)
	}
	if len(hf.Hooks["Custom"]) == 0 {
		t.Fatal("custom hook lost")
	}
}

func TestRegisterHooks_AddsMissingMatcherVariant(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	hooksPath := filepath.Join(dir, "hooks.json")

	err := os.WriteFile(hooksPath, []byte(`{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"/roost event codex"}]}]}}`), 0o644)
	if err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	changed, _, err := RegisterHooks(cfgPath, hooksPath, "/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	raw, _ := os.ReadFile(hooksPath)
	var hf hooksFile
	if err := json.Unmarshal(raw, &hf); err != nil {
		t.Fatalf("unmarshal hooks: %v", err)
	}
	if got := len(hf.Hooks["SessionStart"]); got != 2 {
		t.Fatalf("SessionStart entries = %d, want 2", got)
	}
}

func TestRegisterHooks_UpdatesExistingTimeoutForSameMatcher(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	hooksPath := filepath.Join(dir, "hooks.json")

	err := os.WriteFile(hooksPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/roost event codex","timeout":5}]}]}}`), 0o644)
	if err != nil {
		t.Fatalf("write hooks: %v", err)
	}

	changed, _, err := RegisterHooks(cfgPath, hooksPath, "/roost")
	if err != nil {
		t.Fatalf("RegisterHooks: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}

	raw, _ := os.ReadFile(hooksPath)
	var hf hooksFile
	if err := json.Unmarshal(raw, &hf); err != nil {
		t.Fatalf("unmarshal hooks: %v", err)
	}
	if got := len(hf.Hooks["Stop"]); got != 1 {
		t.Fatalf("Stop entries = %d, want 1", got)
	}
	if got := hf.Hooks["Stop"][0].Hooks[0].Timeout; got != 30 {
		t.Fatalf("timeout = %d, want 30", got)
	}
}

func TestRegisterMCPServer_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")

	added, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	if !added {
		t.Error("expected added=true for new file")
	}

	data, _ := os.ReadFile(path)
	var servers map[string]any
	json.Unmarshal(data, &servers)

	entry, ok := servers["roost-peers"].(map[string]any)
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
	path := filepath.Join(dir, "mcp.json")

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
	path := filepath.Join(dir, "mcp.json")
	os.WriteFile(path, []byte(`{"other-tool":{"command":"other","args":["run"]}}`), 0o644)

	_, err := RegisterMCPServer(path, "/usr/local/bin/roost")
	if err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}

	data, _ := os.ReadFile(path)
	var servers map[string]any
	json.Unmarshal(data, &servers)

	if _, ok := servers["other-tool"]; !ok {
		t.Error("existing other-tool entry was removed")
	}
	if _, ok := servers["roost-peers"]; !ok {
		t.Error("roost-peers was not written")
	}
}
