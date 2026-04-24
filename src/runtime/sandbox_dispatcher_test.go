package runtime

import (
	"context"
	"testing"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/state"
)

// fakeAgentLauncher records calls for assertion in tests.
type fakeAgentLauncher struct {
	wrapLaunchCalled bool
	adoptFrameCalled bool
	shutdownCalled   bool
	wrapErr          error
	adoptErr         error
	wrapResult       WrappedLaunch
}

func (f *fakeAgentLauncher) WrapLaunch(_ state.FrameID, _ state.LaunchPlan, _ map[string]string) (WrappedLaunch, error) {
	f.wrapLaunchCalled = true
	return f.wrapResult, f.wrapErr
}

func (f *fakeAgentLauncher) AdoptFrame(_ context.Context, _ state.FrameID, _ string) (func() error, error) {
	f.adoptFrameCalled = true
	return nil, f.adoptErr
}

func (f *fakeAgentLauncher) Shutdown() error {
	f.shutdownCalled = true
	return nil
}

func TestSandboxDispatcher_DirectMode_RoutesToDirect(t *testing.T) {
	direct := &fakeAgentLauncher{wrapResult: WrappedLaunch{Command: "bash"}}
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "direct"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: direct}

	plan := state.LaunchPlan{Project: "/workspace/foo", Command: "bash"}
	got, err := d.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch error: %v", err)
	}
	if !direct.wrapLaunchCalled {
		t.Error("expected direct WrapLaunch to be called")
	}
	if got.Command != "bash" {
		t.Errorf("Command = %q, want bash", got.Command)
	}
}

func TestSandboxDispatcher_EmptyMode_RoutesToDirect(t *testing.T) {
	direct := &fakeAgentLauncher{}
	resolver := config.NewSandboxResolver(config.SandboxConfig{}) // mode = ""
	d := &SandboxDispatcher{Resolver: resolver, Direct: direct}

	_, err := d.WrapLaunch("f1", state.LaunchPlan{Project: "/workspace/foo"}, nil)
	if err != nil {
		t.Fatalf("WrapLaunch error: %v", err)
	}
	if !direct.wrapLaunchCalled {
		t.Error("expected direct.WrapLaunch called for empty mode")
	}
}

func TestSandboxDispatcher_DockerMode_NilDocker_ReturnsError(t *testing.T) {
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "docker"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: DirectLauncher{}, Docker: nil}

	_, err := d.WrapLaunch("f1", state.LaunchPlan{Project: "/workspace/foo"}, nil)
	if err == nil {
		t.Error("expected error when docker backend is nil but mode=docker")
	}
}

func TestSandboxDispatcher_UnknownMode_ReturnsError(t *testing.T) {
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "firecracker"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: DirectLauncher{}}

	_, err := d.WrapLaunch("f1", state.LaunchPlan{Project: "/workspace/foo"}, nil)
	if err == nil {
		t.Error("expected error for unknown mode")
	}
}

func TestSandboxDispatcher_AdoptFrame_DirectMode(t *testing.T) {
	direct := &fakeAgentLauncher{}
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "direct"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: direct}

	_, err := d.AdoptFrame(context.Background(), "f1", "/workspace/foo")
	if err != nil {
		t.Fatalf("AdoptFrame error: %v", err)
	}
	if !direct.adoptFrameCalled {
		t.Error("expected direct.AdoptFrame called")
	}
}

func TestSandboxDispatcher_Shutdown_CallsBothBackends(t *testing.T) {
	direct := &fakeAgentLauncher{}
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "direct"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: direct}

	if err := d.Shutdown(); err != nil {
		t.Fatalf("Shutdown error: %v", err)
	}
	if !direct.shutdownCalled {
		t.Error("expected direct.Shutdown to be called")
	}
}

func TestSandboxDispatcher_PruneOrphans_NilDocker_NoOp(t *testing.T) {
	resolver := config.NewSandboxResolver(config.SandboxConfig{Mode: "direct"})
	d := &SandboxDispatcher{Resolver: resolver, Direct: DirectLauncher{}}
	// must not panic
	d.PruneOrphans(context.Background(), []string{"/workspace/foo"}, nil)
}
