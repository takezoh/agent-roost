package runtime

import (
	"context"
	"fmt"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/state"
)

// SandboxDispatcher implements AgentLauncher by selecting the correct backend
// (direct or docker) based on the effective sandbox mode for each project.
// The mode is resolved per call via a SandboxResolver so project-scope
// overrides are applied without restarting the daemon.
type SandboxDispatcher struct {
	Resolver *config.SandboxResolver
	Direct   AgentLauncher
	Docker   *DockerLauncher // nil when docker backend is not available
}

// WrapLaunch resolves the effective sandbox mode for plan.Project and
// delegates to the appropriate backend launcher.
func (d *SandboxDispatcher) WrapLaunch(frameID state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error) {
	mode := d.Resolver.Resolve(plan.Project).Mode
	switch mode {
	case "docker":
		if d.Docker == nil {
			return WrappedLaunch{}, fmt.Errorf("sandbox dispatcher: docker mode for %q but docker backend unavailable", plan.Project)
		}
		return d.Docker.WrapLaunch(frameID, plan, env)
	case "", "direct":
		return d.Direct.WrapLaunch(frameID, plan, env)
	default:
		return WrappedLaunch{}, fmt.Errorf("sandbox dispatcher: unknown mode %q for project %q", mode, plan.Project)
	}
}

// AdoptFrame resolves the effective sandbox mode for projectPath and delegates
// to the appropriate backend to reclaim the pre-running sandbox frame.
func (d *SandboxDispatcher) AdoptFrame(ctx context.Context, frameID state.FrameID, projectPath string) (func() error, error) {
	mode := d.Resolver.Resolve(projectPath).Mode
	switch mode {
	case "docker":
		if d.Docker == nil {
			return nil, nil
		}
		return d.Docker.AdoptFrame(ctx, frameID, projectPath)
	case "", "direct":
		return d.Direct.AdoptFrame(ctx, frameID, projectPath)
	default:
		return nil, fmt.Errorf("sandbox dispatcher: unknown mode %q for project %q", mode, projectPath)
	}
}

// Shutdown calls Shutdown on all active backends.
func (d *SandboxDispatcher) Shutdown() error {
	errs := make([]error, 0, 2)
	if err := d.Direct.Shutdown(); err != nil {
		errs = append(errs, err)
	}
	if d.Docker != nil {
		if err := d.Docker.Shutdown(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return fmt.Errorf("sandbox dispatcher: shutdown errors: %v", errs)
}

// PruneOrphans forwards to the docker backend when available.
// resolveImage maps a project path to its currently-effective Docker image.
func (d *SandboxDispatcher) PruneOrphans(ctx context.Context, knownProjects []string, resolveImage func(string) string) {
	if d.Docker != nil {
		d.Docker.PruneOrphans(ctx, knownProjects, resolveImage)
	}
}
