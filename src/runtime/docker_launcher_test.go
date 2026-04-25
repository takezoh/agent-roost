package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/auth/credproxy/awssso"
	"github.com/takezoh/agent-roost/auth/credproxy/gcloudcli"
	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/sandbox"
	sandboxdocker "github.com/takezoh/agent-roost/sandbox/docker"
	"github.com/takezoh/agent-roost/state"

	credproxy "github.com/takezoh/agent-roost/auth/credproxy"
)

func newStubInst() *sandbox.Instance[*sandboxdocker.ContainerState] {
	return &sandbox.Instance[*sandboxdocker.ContainerState]{
		ProjectPath: "/proj",
		Image:       "node:22",
		Internal:    &sandboxdocker.ContainerState{},
	}
}

func resolveSandboxDefault(string) config.SandboxConfig {
	return config.SandboxConfig{Docker: config.DockerConfig{Image: "node:22"}}
}

// newTestRunnerWithProviders creates a CredProxyRunner for tests without starting a server.
func newTestRunnerWithProviders(providers ...credproxy.Provider) *CredProxyRunner {
	return &CredProxyRunner{providers: providers}
}

func TestDockerLauncher_proxyEnvInjected(t *testing.T) {
	awsDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(awsDir, "aws-creds.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	provider := awssso.NewSpecBuilder("127.0.0.1:9787", "test-token", awsDir)
	runner := newTestRunnerWithProviders(provider)

	var capturedOpts sandbox.StartOptions
	mgr := &capturingManager{
		inst: newStubInst(),
		onEnsure: func(opts sandbox.StartOptions) {
			capturedOpts = opts
		},
	}

	resolveSandboxWithAWS := func(string) config.SandboxConfig {
		return config.SandboxConfig{
			Docker: config.DockerConfig{Image: "node:22"},
			Proxy:  config.ProxyConfig{AWSProfiles: []string{"default"}},
		}
	}

	l := NewDockerLauncher(mgr, resolveSandboxWithAWS, runner)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	want := awssso.ContainerEnv("http://host.docker.internal:9787", "test-token")
	for k, v := range want {
		if capturedOpts.Env[k] != v {
			t.Errorf("env[%q] = %q, want %q", k, capturedOpts.Env[k], v)
		}
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

	l := NewDockerLauncher(mgr, resolveSandboxDefault, nil)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	if capturedOpts.Env["AWS_CONTAINER_CREDENTIALS_FULL_URI"] != "" {
		t.Errorf("AWS_CONTAINER_CREDENTIALS_FULL_URI should not be set when proxy is disabled")
	}
}

func TestDockerLauncher_noGCPConfig_noGCPEnv(t *testing.T) {
	awsDir := t.TempDir()
	provider := awssso.NewSpecBuilder("127.0.0.1:9787", "test-token", awsDir)
	runner := newTestRunnerWithProviders(provider)

	var capturedOpts sandbox.StartOptions
	mgr := &capturingManager{
		inst: newStubInst(),
		onEnsure: func(opts sandbox.StartOptions) {
			capturedOpts = opts
		},
	}

	l := NewDockerLauncher(mgr, resolveSandboxDefault, runner)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	// No GCP account configured — CLOUDSDK_CONFIG must not be injected.
	if capturedOpts.Env["CLOUDSDK_CONFIG"] != "" {
		t.Errorf("CLOUDSDK_CONFIG should not be set when gcp.account is empty")
	}
}

func TestDockerLauncher_withGCPConfig_injectsEnv(t *testing.T) {
	// Stub gcloud so token refresh doesn't fail.
	stubDir := t.TempDir()
	writeStubGcloud(t, stubDir+"/gcloud", "test-token-xyz")
	t.Setenv("PATH", stubDir+":"+os.Getenv("PATH"))

	gcpDir := t.TempDir()
	gcpProvider := gcloudcli.NewSpecBuilder(context.Background(), gcpDir)
	runner := newTestRunnerWithProviders(gcpProvider)

	var capturedOpts sandbox.StartOptions
	mgr := &capturingManager{
		inst: newStubInst(),
		onEnsure: func(opts sandbox.StartOptions) {
			capturedOpts = opts
		},
	}

	resolveSandboxWithGCP := func(string) config.SandboxConfig {
		return config.SandboxConfig{
			Docker: config.DockerConfig{Image: "node:22"},
			Proxy: config.ProxyConfig{
				GCP: config.GCPConfig{
					Account:  "user@example.com",
					Projects: []string{"proj-a"},
				},
			},
		}
	}

	l := NewDockerLauncher(mgr, resolveSandboxWithGCP, runner)
	plan := state.LaunchPlan{Command: "claude", StartDir: "/proj", Project: "/proj"}
	_, err := l.WrapLaunch("f1", plan, nil)
	if err != nil {
		t.Fatalf("WrapLaunch: %v", err)
	}

	if capturedOpts.Env["CLOUDSDK_CONFIG"] == "" {
		t.Errorf("CLOUDSDK_CONFIG should be set when gcp.account is configured")
	}

	// At least one mount should reference gcloud-config.
	foundConfigMount := false
	for _, m := range capturedOpts.ExtraMounts {
		if strings.Contains(m, "gcloud-config") {
			foundConfigMount = true
		}
	}
	if !foundConfigMount {
		t.Errorf("expected a gcloud-config mount, got: %v", capturedOpts.ExtraMounts)
	}
}

func writeStubGcloud(t *testing.T, path, token string) {
	t.Helper()
	content := "#!/bin/sh\necho " + token + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write stub gcloud: %v", err)
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
