// Package sshagent implements a credproxy.Provider that forwards the host SSH
// agent socket into the container. No HTTP route is involved; the agent socket
// is bind-mounted directly and SSH_AUTH_SOCK is set to the container path.
package sshagent

import (
	"context"
	"log/slog"
	"os"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
	"github.com/takezoh/agent-roost/config"
	credproxylib "github.com/takezoh/credproxy/pkg/credproxy"
)

const containerSocketPath = "/opt/roost/ssh-agent.sock"

// SpecBuilder implements credproxy.Provider for SSH agent forwarding.
type SpecBuilder struct{}

// NewSpecBuilder creates a SpecBuilder.
func NewSpecBuilder() *SpecBuilder { return &SpecBuilder{} }

func (b *SpecBuilder) Name() string { return "sshagent" }

// Init is a no-op; this provider needs no persistent host-side files.
func (b *SpecBuilder) Init() error { return nil }

// Routes returns nil; this provider uses a bind-mounted socket, not an HTTP route.
func (b *SpecBuilder) Routes() []credproxylib.Route { return nil }

// ContainerSpec implements credproxy.Provider.
// Returns zero Spec when proxy.ssh_agent.forward is false or $SSH_AUTH_SOCK is absent.
func (b *SpecBuilder) ContainerSpec(_ context.Context, _ string, sb config.SandboxConfig) (credproxy.Spec, error) {
	if !sb.Proxy.SSHAgent.Forward {
		return credproxy.Spec{}, nil
	}

	sockPath := os.Getenv("SSH_AUTH_SOCK")
	if sockPath == "" {
		slog.Warn("sshagent: forward=true but SSH_AUTH_SOCK is not set")
		return credproxy.Spec{}, nil
	}
	if _, err := os.Stat(sockPath); err != nil {
		slog.Warn("sshagent: SSH_AUTH_SOCK socket not found", "path", sockPath)
		return credproxy.Spec{}, nil
	}

	return credproxy.Spec{
		Env: map[string]string{
			"SSH_AUTH_SOCK": containerSocketPath,
		},
		Mounts: []string{sockPath + ":" + containerSocketPath},
	}, nil
}
