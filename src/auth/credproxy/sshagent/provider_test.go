package sshagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/takezoh/agent-roost/config"
)

func cfg(forward bool) config.SandboxConfig {
	return config.SandboxConfig{
		Proxy: config.ProxyConfig{
			SSHAgent: config.SSHAgentConfig{Forward: forward},
		},
	}
}

func TestSpecBuilder_forward_false(t *testing.T) {
	b := NewSpecBuilder()
	spec, err := b.ContainerSpec(context.Background(), "/proj", cfg(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 || len(spec.Mounts) != 0 {
		t.Errorf("expected zero spec, got %+v", spec)
	}
}

func TestSpecBuilder_forward_true_no_sock_env(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	b := NewSpecBuilder()
	spec, err := b.ContainerSpec(context.Background(), "/proj", cfg(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 {
		t.Errorf("expected zero spec when SSH_AUTH_SOCK unset, got %+v", spec)
	}
}

func TestSpecBuilder_forward_true_sock_missing(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/nonexistent/path/agent.sock")
	b := NewSpecBuilder()
	spec, err := b.ContainerSpec(context.Background(), "/proj", cfg(true))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Env) != 0 {
		t.Errorf("expected zero spec when socket absent, got %+v", spec)
	}
}

func TestSpecBuilder_forward_true_sock_present(t *testing.T) {
	// Use a regular file to satisfy os.Stat (socket creation is not needed for this logic test).
	sockPath := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(sockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("SSH_AUTH_SOCK", sockPath)
	b := NewSpecBuilder()
	spec, err := b.ContainerSpec(context.Background(), "/proj", cfg(true))
	if err != nil {
		t.Fatal(err)
	}

	if spec.Env["SSH_AUTH_SOCK"] != containerSocketPath {
		t.Errorf("SSH_AUTH_SOCK = %q, want %q", spec.Env["SSH_AUTH_SOCK"], containerSocketPath)
	}
	wantMount := sockPath + ":" + containerSocketPath
	if len(spec.Mounts) != 1 || spec.Mounts[0] != wantMount {
		t.Errorf("mounts = %v, want [%s]", spec.Mounts, wantMount)
	}
}
