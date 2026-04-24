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
// All driver kinds are run inside Docker; docker config is resolved per
// project via the resolveDocker callback (user + project scope merge).
type DockerLauncher struct {
	mgr           sandbox.Manager[*sandboxdocker.ContainerState]
	resolveDocker func(projectPath string) config.DockerConfig
}

// NewDockerLauncher creates an AgentLauncher that runs agents inside Docker.
// resolveDocker is called per launch to obtain the effective docker config
// for a project (user scope merged with optional project scope override).
func NewDockerLauncher(mgr sandbox.Manager[*sandboxdocker.ContainerState], resolveDocker func(string) config.DockerConfig) *DockerLauncher {
	return &DockerLauncher{mgr: mgr, resolveDocker: resolveDocker}
}

// WrapLaunch ensures the project container is running, then returns a launch
// spec that runs the agent via "docker exec". The Cleanup callback releases
// the ref-count and destroys the container when the last frame exits.
func (l *DockerLauncher) WrapLaunch(frameID state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error) {
	if plan.Project == "" {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: plan.Project is empty for frame %s", frameID)
	}

	dockerCfg := l.resolveDocker(plan.Project)
	opts := sandbox.StartOptions{
		ExtraMounts: dockerCfg.ExtraMounts,
		Env:         dockerCfg.Env,
		ForwardEnv:  dockerCfg.ForwardEnv,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	inst, err := l.mgr.EnsureInstance(ctx, plan.Project, dockerCfg.Image, opts)
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
// a pre-running frame. It calls EnsureInstance (which reclaims the running
// container if it is still alive, or starts a fresh one if it died) and
// acquires a ref-count for the frame.
func (l *DockerLauncher) AdoptFrame(ctx context.Context, frameID state.FrameID, projectPath string) (func() error, error) {
	if projectPath == "" {
		return nil, nil
	}
	dockerCfg := l.resolveDocker(projectPath)
	opts := sandbox.StartOptions{
		ExtraMounts: dockerCfg.ExtraMounts,
		Env:         dockerCfg.Env,
		ForwardEnv:  dockerCfg.ForwardEnv,
	}
	inst, err := l.mgr.EnsureInstance(ctx, projectPath, dockerCfg.Image, opts)
	if err != nil {
		return nil, fmt.Errorf("docker launcher: adopt frame %s: %w", frameID, err)
	}
	l.mgr.AcquireFrame(inst)
	slog.Debug("docker launcher: frame adopted (warm start)", "frame", frameID, "project", projectPath, "image", inst.Image)
	return l.makeCleanup(frameID, inst), nil
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
// resolveImage maps a project path to the currently-effective image.
func (l *DockerLauncher) PruneOrphans(ctx context.Context, knownProjects []string, resolveImage func(string) string) {
	l.mgr.PruneOrphans(ctx, knownProjects, resolveImage)
}
