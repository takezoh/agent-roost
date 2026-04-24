package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/takezoh/agent-roost/sandbox"
	"github.com/takezoh/agent-roost/state"
)

// DockerLauncher wraps launches inside per-project Docker containers.
// It implements AgentLauncher by delegating to a sandbox.Manager.
type DockerLauncher struct {
	mgr sandbox.Manager
}

// NewDockerLauncher creates an AgentLauncher that runs agents inside Docker.
func NewDockerLauncher(mgr sandbox.Manager) *DockerLauncher {
	return &DockerLauncher{mgr: mgr}
}

// WrapLaunch ensures the project container is running, then returns a launch
// spec that runs the agent via "docker exec". The Cleanup callback releases
// the ref-count and destroys the container when the last frame exits.
//
// Non-shell drivers (claude, codex, gemini, …) are not sandbox-capable in P2.1
// and fall back to DirectLauncher so they run on the host unchanged.
func (l *DockerLauncher) WrapLaunch(frameID state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error) {
	if plan.Command != "shell" {
		slog.Info("docker launcher: non-shell driver uses direct launch", "frame", frameID, "command", plan.Command)
		return DirectLauncher{}.WrapLaunch(frameID, plan, env)
	}

	if plan.Project == "" {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: plan.Project is empty for frame %s", frameID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	inst, err := l.mgr.EnsureInstance(ctx, plan.Project)
	if err != nil {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: ensure instance: %w", err)
	}

	cmd, outEnv, err := l.mgr.BuildLaunchCommand(inst, plan, env)
	if err != nil {
		return WrappedLaunch{}, fmt.Errorf("docker launcher: build command: %w", err)
	}

	l.mgr.AcquireFrame(inst)
	slog.Debug("docker launcher: frame acquired", "frame", frameID, "project", plan.Project)

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
	inst, err := l.mgr.EnsureInstance(ctx, projectPath)
	if err != nil {
		return nil, fmt.Errorf("docker launcher: adopt frame %s: %w", frameID, err)
	}
	l.mgr.AcquireFrame(inst)
	slog.Debug("docker launcher: frame adopted (warm start)", "frame", frameID, "project", projectPath)
	return l.makeCleanup(frameID, inst), nil
}

func (l *DockerLauncher) makeCleanup(frameID state.FrameID, inst *sandbox.Instance) func() error {
	return func() error {
		shouldDestroy := l.mgr.ReleaseFrame(inst)
		slog.Debug("docker launcher: frame released", "frame", frameID, "project", inst.ProjectPath, "destroy", shouldDestroy)
		if !shouldDestroy {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return l.mgr.DestroyInstance(ctx, inst)
	}
}

// Shutdown is a no-op for the Docker backend: containers must survive daemon
// shutdown so that tmux panes stay alive for warm-restart adoption.
func (l *DockerLauncher) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return l.mgr.Shutdown(ctx)
}

// PruneOrphans removes roost-managed Docker containers that are not
// associated with any of knownProjects. Call once at startup after loading
// the session snapshot.
func (l *DockerLauncher) PruneOrphans(ctx context.Context, knownProjects []string) {
	l.mgr.PruneOrphans(ctx, knownProjects)
}
