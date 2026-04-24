package docker

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/takezoh/agent-roost/sandbox"
	"github.com/takezoh/agent-roost/state"
)

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not found in PATH")
	}
	if out, err := exec.Command("docker", "version", "--format", "ok").Output(); err != nil ||
		strings.TrimSpace(string(out)) != "ok" {
		t.Skip("docker daemon not available")
	}
}

func TestContainerName_deterministic(t *testing.T) {
	a := containerName("/workspace/foo", "alpine:latest")
	b := containerName("/workspace/foo", "alpine:latest")
	if a != b {
		t.Errorf("container name not deterministic: %q != %q", a, b)
	}
	c := containerName("/workspace/bar", "alpine:latest")
	if a == c {
		t.Errorf("different projects produced same container name: %q", a)
	}
	if !strings.HasPrefix(a, "roost-") {
		t.Errorf("container name missing roost- prefix: %q", a)
	}
}

func TestContainerName_imageDistinct(t *testing.T) {
	a := containerName("/workspace/foo", "alpine:latest")
	b := containerName("/workspace/foo", "ubuntu:22.04")
	if a == b {
		t.Errorf("same project with different images produced same container name: %q", a)
	}
}

func TestShellEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/workspace/foo bar", "'/workspace/foo bar'"},
		{"/no/special", "'/no/special'"},
		{"it's", `'it'\''s'`},
	}
	for _, tt := range cases {
		got := shellEscape(tt.in)
		if got != tt.want {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolveImage_priority(t *testing.T) {
	// user override wins
	if got := resolveImage("/any", "custom:tag"); got != "custom:tag" {
		t.Errorf("user image not respected: %q", got)
	}
	// no devcontainer.json → default
	if got := resolveImage("/nonexistent/path/xyz123", ""); got != defaultImage {
		t.Errorf("default image not returned: %q", got)
	}
}

func TestBuildLaunchCommand(t *testing.T) {
	mgr := New(Config{Network: "test-net"})
	cs := &containerState{name: "roost-abc123"}
	inst := &sandbox.Instance{
		ProjectPath: "/workspace/foo",
		Internal:    cs,
	}
	plan := state.LaunchPlan{
		Command:  "bash",
		StartDir: "/workspace/foo",
		Project:  "/workspace/foo",
	}
	env := map[string]string{"ROOST_FRAME_ID": "f1"}

	cmd, outEnv, err := mgr.BuildLaunchCommand(inst, plan, env)
	if err != nil {
		t.Fatalf("BuildLaunchCommand: %v", err)
	}
	if !strings.Contains(cmd, "docker exec -it") {
		t.Errorf("command missing 'docker exec -it': %q", cmd)
	}
	if !strings.Contains(cmd, cs.name) {
		t.Errorf("command missing container name %q: %q", cs.name, cmd)
	}
	if !strings.Contains(cmd, "ROOST_FRAME_ID") {
		t.Errorf("command missing env var ROOST_FRAME_ID: %q", cmd)
	}
	if len(outEnv) != 0 {
		t.Errorf("outEnv should be empty (env is in -e flags), got %v", outEnv)
	}
}

func TestBuildLaunchCommand_shellTranslated(t *testing.T) {
	mgr := New(Config{Network: "test-net"})
	cs := &containerState{name: "roost-abc123"}
	inst := &sandbox.Instance{ProjectPath: "/workspace/foo", Internal: cs}
	plan := state.LaunchPlan{Command: "shell", StartDir: "/workspace/foo", Project: "/workspace/foo"}

	cmd, _, err := mgr.BuildLaunchCommand(inst, plan, nil)
	if err != nil {
		t.Fatalf("BuildLaunchCommand: %v", err)
	}
	if !strings.HasSuffix(cmd, "/bin/bash") {
		t.Errorf("'shell' not translated to /bin/bash: %q", cmd)
	}
}

func TestRefCount(t *testing.T) {
	mgr := New(Config{})
	cs := &containerState{name: "test"}
	inst := &sandbox.Instance{ProjectPath: "/proj", Internal: cs}

	mgr.AcquireFrame(inst)
	mgr.AcquireFrame(inst)
	if mgr.ReleaseFrame(inst) {
		t.Error("ReleaseFrame should return false when count still > 0")
	}
	if !mgr.ReleaseFrame(inst) {
		t.Error("ReleaseFrame should return true when count drops to 0")
	}
}

func TestEnsureInstance_docker(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()
	opts := sandbox.StartOptions{}

	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", opts)
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	if inst.ProjectPath != "/tmp" {
		t.Errorf("ProjectPath = %q, want /tmp", inst.ProjectPath)
	}
	if inst.Image != "alpine:latest" {
		t.Errorf("Image = %q, want alpine:latest", inst.Image)
	}

	// Second call returns same container (idempotent).
	inst2, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", opts)
	if err != nil {
		t.Fatalf("EnsureInstance (2nd): %v", err)
	}
	cs1 := inst.Internal.(*containerState)
	cs2 := inst2.Internal.(*containerState)
	if cs1.name != cs2.name {
		t.Errorf("second call returned different container: %q vs %q", cs1.name, cs2.name)
	}

	_ = mgr.DestroyInstance(ctx, inst)
}

func TestShutdown_PreservesContainers(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()

	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", sandbox.StartOptions{})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	cs := inst.Internal.(*containerState)
	name := cs.name
	t.Cleanup(func() { _, _ = exec.Command("docker", "kill", name).CombinedOutput() })

	// Shutdown must NOT kill the container.
	if err := mgr.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	running, err := isContainerRunning(ctx, name)
	if err != nil {
		t.Fatalf("isContainerRunning: %v", err)
	}
	if !running {
		t.Errorf("container %q should still be running after Shutdown (warm-restart requirement)", name)
	}
}

func TestPruneOrphans_removesUnknown(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()

	// Start a container for /tmp (will be treated as orphan since we pass empty known list).
	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", sandbox.StartOptions{})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	cs := inst.Internal.(*containerState)
	name := cs.name
	t.Cleanup(func() { _, _ = exec.Command("docker", "kill", name).CombinedOutput() })

	// PruneOrphans with empty known list → container should be killed.
	mgr.PruneOrphans(ctx, nil, nil)

	running, err := isContainerRunning(ctx, name)
	if err != nil {
		t.Fatalf("isContainerRunning: %v", err)
	}
	if running {
		t.Errorf("container %q should have been pruned as orphan", name)
	}
}

func TestPruneOrphans_keepsKnown(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()

	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", sandbox.StartOptions{})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	cs := inst.Internal.(*containerState)
	name := cs.name
	t.Cleanup(func() { _, _ = exec.Command("docker", "kill", name).CombinedOutput() })

	// PruneOrphans with /tmp as known project and matching image → container must survive.
	mgr.PruneOrphans(ctx, []string{"/tmp"}, func(_ string) string { return "alpine:latest" })

	running, err := isContainerRunning(ctx, name)
	if err != nil {
		t.Fatalf("isContainerRunning: %v", err)
	}
	if !running {
		t.Errorf("container %q should have been kept (project is known)", name)
	}
	_ = mgr.DestroyInstance(ctx, inst)
}

func TestPruneOrphans_ImageMismatch_Prunes(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()

	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", sandbox.StartOptions{})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	cs := inst.Internal.(*containerState)
	name := cs.name
	t.Cleanup(func() { _, _ = exec.Command("docker", "kill", name).CombinedOutput() })

	// resolveImage returns a different image → container is stale → must be pruned.
	mgr.PruneOrphans(ctx, []string{"/tmp"}, func(_ string) string { return "node:22-bookworm-slim" })

	running, err := isContainerRunning(ctx, name)
	if err != nil {
		t.Fatalf("isContainerRunning: %v", err)
	}
	if running {
		t.Errorf("container %q should have been pruned (image mismatch)", name)
	}
}

func TestPruneOrphans_ImageMatch_Keeps(t *testing.T) {
	skipIfNoDocker(t)

	mgr := New(Config{Network: "bridge"})
	ctx := context.Background()

	inst, err := mgr.EnsureInstance(ctx, "/tmp", "alpine:latest", sandbox.StartOptions{})
	if err != nil {
		t.Fatalf("EnsureInstance: %v", err)
	}
	cs := inst.Internal.(*containerState)
	name := cs.name
	t.Cleanup(func() { _, _ = exec.Command("docker", "kill", name).CombinedOutput() })

	// resolveImage returns the same image → container is still current → must survive.
	mgr.PruneOrphans(ctx, []string{"/tmp"}, func(_ string) string { return "alpine:latest" })

	running, err := isContainerRunning(ctx, name)
	if err != nil {
		t.Fatalf("isContainerRunning: %v", err)
	}
	if !running {
		t.Errorf("container %q should have been kept (image matches)", name)
	}
	_ = mgr.DestroyInstance(ctx, inst)
}
