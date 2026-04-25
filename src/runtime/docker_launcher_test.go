package runtime

import (
	"context"
	"testing"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/sandbox"
	sandboxdocker "github.com/takezoh/agent-roost/sandbox/docker"
	"github.com/takezoh/agent-roost/state"
)

func newStubInst() *sandbox.Instance[*sandboxdocker.ContainerState] {
	return &sandbox.Instance[*sandboxdocker.ContainerState]{
		ProjectPath: "/proj",
		Image:       "node:22",
		Internal:    &sandboxdocker.ContainerState{},
	}
}

func resolveDockerDefault(string) config.DockerConfig {
	return config.DockerConfig{Image: "node:22"}
}

// newTestRunner creates a CredProxyRunner for tests without starting an actual server.
func newTestRunner(addr, token string) *CredProxyRunner {
	return &CredProxyRunner{addr: addr, token: token}
}

func TestDockerLauncher_proxyEnvInjected(t *testing.T) {
	runner := newTestRunner("127.0.0.1:9787", "test-token")

	var capturedOpts sandbox.StartOptions
	mgr := &capturingManager{
		inst: newStubInst(),
		onEnsure: func(opts sandbox.StartOptions) {
			capturedOpts = opts
		},
	}

	l := NewDockerLauncher(mgr, resolveDockerDefault, runner)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	if capturedOpts.Env["ANTHROPIC_BASE_URL"] != "http://host.docker.internal:9787/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", capturedOpts.Env["ANTHROPIC_BASE_URL"])
	}
	if capturedOpts.Env["ANTHROPIC_AUTH_TOKEN"] != "test-token" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", capturedOpts.Env["ANTHROPIC_AUTH_TOKEN"])
	}
	if capturedOpts.Env["AWS_CONTAINER_CREDENTIALS_FULL_URI"] != "http://host.docker.internal:9787/aws-credentials" {
		t.Errorf("AWS_CONTAINER_CREDENTIALS_FULL_URI = %q", capturedOpts.Env["AWS_CONTAINER_CREDENTIALS_FULL_URI"])
	}
}

func TestDockerLauncher_noProxy_noEnv(t *testing.T) {
	var capturedOpts sandbox.StartOptions
	mgr := &capturingManager{
		inst: newStubInst(),
		onEnsure: func(opts sandbox.StartOptions) {
			capturedOpts = opts
		},
	}

	l := NewDockerLauncher(mgr, resolveDockerDefault, nil)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	if capturedOpts.Env["ANTHROPIC_BASE_URL"] != "" {
		t.Errorf("ANTHROPIC_BASE_URL should not be set when proxy is disabled")
	}
}

// capturingManager calls onEnsure to capture StartOptions passed to EnsureInstance.
type capturingManager struct {
	inst     *sandbox.Instance[*sandboxdocker.ContainerState]
	onEnsure func(sandbox.StartOptions)
}

func (c *capturingManager) EnsureInstance(_ context.Context, _ string, _ string, opts sandbox.StartOptions) (*sandbox.Instance[*sandboxdocker.ContainerState], error) {
	if c.onEnsure != nil {
		c.onEnsure(opts)
	}
	return c.inst, nil
}

func (c *capturingManager) BuildLaunchCommand(_ *sandbox.Instance[*sandboxdocker.ContainerState], plan state.LaunchPlan, _ map[string]string) (string, map[string]string, error) {
	return "docker exec -it stub " + plan.Command, map[string]string{}, nil
}

func (c *capturingManager) AcquireFrame(_ *sandbox.Instance[*sandboxdocker.ContainerState]) {}

func (c *capturingManager) ReleaseFrame(_ *sandbox.Instance[*sandboxdocker.ContainerState]) bool {
	return false
}

func (c *capturingManager) DestroyInstance(_ context.Context, _ *sandbox.Instance[*sandboxdocker.ContainerState]) error {
	return nil
}

func (c *capturingManager) PruneOrphans(_ context.Context, _ []string, _ func(string) string) {}
