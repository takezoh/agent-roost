package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/sandbox"
	sandboxdocker "github.com/takezoh/agent-roost/sandbox/docker"
	"github.com/takezoh/agent-roost/state"
)

// DockerLauncher wraps launches inside per-project Docker containers.
// It implements AgentLauncher by delegating to a sandbox.Manager[*docker.ContainerState].
// All driver kinds are run inside Docker; the effective sandbox config is resolved per
// project via the resolveSandbox callback (user + project scope merge).
type DockerLauncher struct {
	mgr            sandbox.Manager[*sandboxdocker.ContainerState]
	resolveSandbox func(projectPath string) config.SandboxConfig
	proxy          *CredProxyRunner // nil when proxy disabled
}

// NewDockerLauncher creates an AgentLauncher that runs agents inside Docker.
// resolveSandbox is called per launch to obtain the effective sandbox config.
// proxy is non-nil when in-process credproxy is active.
func NewDockerLauncher(
	mgr sandbox.Manager[*sandboxdocker.ContainerState],
	resolveSandbox func(string) config.SandboxConfig,
	proxy *CredProxyRunner,
) *DockerLauncher {
	return &DockerLauncher{
		mgr:            mgr,
		resolveSandbox: resolveSandbox,
		proxy:          proxy,
	}
}

// WrapLaunch ensures the project container is running, then returns a launch
// spec that runs the agent via "docker exec". The Cleanup callback releases
// the ref-count and destroys the container when the last frame exits.
func (l *DockerLauncher) WrapLaunch(frameID state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error) {
	if plan.Project == "" {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: plan.Project is empty for frame %s", frameID)
	}

	sb := l.resolveSandbox(plan.Project)
	opts := l.buildStartOptions(sb, plan.Project)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	inst, err := l.mgr.EnsureInstance(ctx, plan.Project, sb.Docker.Image, opts)
	if err != nil {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: ensure instance: %w", err)
	}

	cmd, outEnv, err := l.mgr.BuildLaunchCommand(inst, plan, env)
	if err != nil {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: build command: %w", err)
	}

	l.mgr.AcquireFrame(inst)
	slog.Debug("docker launcher: frame acquired", "frame", frameID, "project", plan.Project, "image", inst.Image)

	return WrappedLaunch{
		Command:  cmd,
		StartDir: plan.StartDir,
		Env:      outEnv,
		Cleanup:  l.makeCleanup(frameID, inst),
	}, nil
}

// AdoptFrame is called during warm start to reclaim an existing container for
// a pre-running frame.
func (l *DockerLauncher) AdoptFrame(ctx context.Context, frameID state.FrameID, projectPath string) (func() error, error) {
	if projectPath == "" {
		return nil, nil
	}
	sb := l.resolveSandbox(projectPath)
	opts := l.buildStartOptions(sb, projectPath)
	inst, err := l.mgr.EnsureInstance(ctx, projectPath, sb.Docker.Image, opts)
	if err != nil {
		return nil, fmt.Errorf("docker launcher: adopt frame %s: %w", frameID, err)
	}
	l.mgr.AcquireFrame(inst)
	slog.Debug("docker launcher: frame adopted (warm start)", "frame", frameID, "project", projectPath, "image", inst.Image)
	return l.makeCleanup(frameID, inst), nil
}

// buildStartOptions assembles sandbox.StartOptions from the resolved sandbox config.
func (l *DockerLauncher) buildStartOptions(sb config.SandboxConfig, projectPath string) sandbox.StartOptions {
	opts := sandbox.StartOptions{
		ExtraMounts: sb.Docker.ExtraMounts,
		Env:         sb.Docker.Env,
		ForwardEnv:  sb.Docker.ForwardEnv,
	}
	if l.proxy != nil {
		l.injectProxy(&opts, projectPath, sb)
	}
	return opts
}

// injectProxy merges credential provider env and mounts into StartOptions.
// All provider-specific logic is encapsulated in CredProxyRunner.ContainerSpec.
func (l *DockerLauncher) injectProxy(opts *sandbox.StartOptions, projectPath string, sb config.SandboxConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	spec, err := l.proxy.ContainerSpec(ctx, projectPath, sb)
	if err != nil {
		slog.Warn("docker launcher: credproxy spec failed", "project", projectPath, "err", err)
		return
	}
	if opts.Env == nil {
		opts.Env = make(map[string]string)
	}
	for k, v := range spec.Env {
		opts.Env[k] = v
	}
	opts.ExtraMounts = append(opts.ExtraMounts, spec.Mounts...)
}

func (l *DockerLauncher) makeCleanup(frameID state.FrameID, inst *sandbox.Instance[*sandboxdocker.ContainerState]) func() error {
	return func() error {
		shouldDestroy := l.mgr.ReleaseFrame(inst)
		slog.Debug("docker launcher: frame released", "frame", frameID, "project", inst.ProjectPath, "image", inst.Image, "destroy", shouldDestroy)
		if !shouldDestroy {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return l.mgr.DestroyInstance(ctx, inst)
	}
}

// PruneOrphans removes roost-managed Docker containers whose project is not
// in knownProjects, or whose image no longer matches the resolved config.
func (l *DockerLauncher) PruneOrphans(ctx context.Context, knownProjects []string, resolveImage func(string) string) {
	l.mgr.PruneOrphans(ctx, knownProjects, resolveImage)
}
