package runtime

import (
	"context"

	"github.com/takezoh/agent-roost/state"
)

// WrappedLaunch is the resolved launch specification after the launcher
// has applied any sandboxing. The runtime passes Command/StartDir/Env
// directly to TmuxBackend.SpawnWindow; Cleanup is called when the frame
// is destroyed (nil is safe to ignore).
type WrappedLaunch struct {
	Command  string
	StartDir string
	Env      map[string]string
	Cleanup  func() error
}

// AgentLauncher wraps a state.LaunchPlan before it reaches tmux, allowing
// sandbox implementations (bwrap, Firecracker, …) to prepend wrapper
// commands or spin up VMs. The runtime calls WrapLaunch once per spawn;
// DirectLauncher is used when no Launcher is configured.
type AgentLauncher interface {
	WrapLaunch(frameID state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error)

	// AdoptFrame is called during warm start to re-register a pre-existing frame
	// with the sandbox backend (the agent process is already running in tmux).
	// driverKind (e.g. "shell", "claude") is used to resolve the sandbox image.
	// Returns the Cleanup callback that should be stored for the frame, or nil
	// if no cleanup is needed. Must not start or restart the sandbox.
	AdoptFrame(ctx context.Context, frameID state.FrameID, projectPath, driverKind string) (func() error, error)

	Shutdown() error
}

// DirectLauncher is the no-op implementation: it passes the plan through
// unchanged so behaviour is identical to the pre-launcher code path.
type DirectLauncher struct{}

func (DirectLauncher) WrapLaunch(_ state.FrameID, plan state.LaunchPlan, env map[string]string) (WrappedLaunch, error) {
	return WrappedLaunch{
		Command:  plan.Command,
		StartDir: plan.StartDir,
		Env:      env,
	}, nil
}

func (DirectLauncher) AdoptFrame(_ context.Context, _ state.FrameID, _, _ string) (func() error, error) {
	return nil, nil
}

func (DirectLauncher) Shutdown() error { return nil }

// launcher returns cfg.Launcher if set, otherwise a zero-cost DirectLauncher.
func launcher(cfg Config) AgentLauncher {
	if cfg.Launcher != nil {
		return cfg.Launcher
	}
	return DirectLauncher{}
}
