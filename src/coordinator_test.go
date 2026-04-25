package main

import (
	"context"
	"runtime"
	"testing"

	"github.com/takezoh/agent-roost/config"
	appruntime "github.com/takezoh/agent-roost/runtime"
)

func TestNewAgentLauncher_direct(t *testing.T) {
	for _, mode := range []string{"", "direct"} {
		l, err := newAgentLauncher(context.Background(), config.SandboxConfig{Mode: mode})
		if err != nil {
			t.Errorf("mode=%q: unexpected error: %v", mode, err)
			continue
		}
		// newAgentLauncher always returns a SandboxDispatcher; for direct mode
		// the Docker backend must be nil (no docker spawning).
		d, ok := l.(*appruntime.SandboxDispatcher)
		if !ok {
			t.Errorf("mode=%q: expected *SandboxDispatcher, got %T", mode, l)
			continue
		}
		if d.Docker != nil {
			t.Errorf("mode=%q: expected Docker=nil for direct mode, got %T", mode, d.Docker)
		}
	}
}

func TestNewAgentLauncher_docker_missing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH manipulation unreliable on windows")
	}
	t.Setenv("PATH", "")
	_, err := newAgentLauncher(context.Background(), config.SandboxConfig{Mode: "docker"})
	if err == nil {
		t.Error("expected error when docker is not in PATH, got nil")
	}
}

func TestResolveShellDisplayFromValues(t *testing.T) {
	cases := []struct {
		tmuxDefault string
		envSHELL    string
		want        string
	}{
		{"/usr/bin/zsh", "/bin/bash", "zsh"},
		{"", "/bin/bash", "bash"},
		{"", "/usr/bin/zsh", "zsh"},
		{"", "", "shell"},
		{".", "", "shell"},
		{"", ".", "shell"},
		{".", ".", "shell"},
	}
	for _, c := range cases {
		got := resolveShellDisplayFromValues(c.tmuxDefault, c.envSHELL)
		if got != c.want {
			t.Errorf("resolveShellDisplayFromValues(%q, %q) = %q, want %q",
				c.tmuxDefault, c.envSHELL, got, c.want)
		}
	}
}
