package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFrom_SSHAgentForward(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.toml")
	os.WriteFile(path, []byte(`
[sandbox.proxy]
enabled = true
ssh_agent.forward = true
`), 0o644)

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sandbox.Proxy.SSHAgent.Forward {
		t.Errorf("SSHAgent.Forward = false, want true")
	}
}
