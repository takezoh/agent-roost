package credproxy

import (
	"context"

	"github.com/takezoh/agent-roost/config"
)

// Spec is the per-container contribution from a single credential provider.
// Env keys are roost-internal names; Mounts are docker-style "host:guest[:mode]" specs.
type Spec struct {
	Env    map[string]string
	Mounts []string
}

// Provider is implemented by each credential backend (awssso, gcloudcli, ...).
// ContainerSpec returns this provider's contribution for projectPath, or a zero
// Spec when the provider is not configured for that project.
type Provider interface {
	ContainerSpec(ctx context.Context, projectPath string, sb config.SandboxConfig) (Spec, error)
}
