// Package docker implements the sandbox.Manager interface using Docker containers.
//
// Each project gets one long-lived container (roost-<slug>) that idles with
// "sleep infinity". Frames join via "docker exec -it". The container is stopped
// when the last frame's Cleanup runs.
package docker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/takezoh/agent-roost/sandbox"
	"github.com/takezoh/agent-roost/state"
)

// Config carries the docker-specific tunable parameters from config.toml.
// The container image and per-driver mounts/env are provided at launch time
// via sandbox.StartOptions, not stored here.
type Config struct {
	Network   string   // docker network name; empty → "roost-sandbox"
	ExtraArgs []string // extra args appended to "docker run"
}

// containerState holds the runtime data for one (project, image) container.
type containerState struct {
	mu       sync.Mutex
	name     string // docker container name
	image    string // resolved docker image
	refCount int
}

// containerKey is the composite key used to identify a container.
func containerKey(projectPath, image string) string { return projectPath + "\t" + image }

// Manager is the Docker implementation of sandbox.Manager.
type Manager struct {
	cfg      Config
	mu       sync.Mutex
	inflight singleflight.Group
	// containers maps containerKey(projectPath, image) → container state
	containers map[string]*containerState
}

// New returns a Manager that owns Docker containers for project sandboxes.
func New(cfg Config) *Manager {
	if cfg.Network == "" {
		cfg.Network = "roost-sandbox"
	}
	return &Manager{
		cfg:        cfg,
		containers: map[string]*containerState{},
	}
}

// EnsureInstance returns the running container for the (projectPath, configImage) pair,
// starting one if necessary. configImage may be empty (Manager falls back to
// devcontainer.json or the built-in default). Concurrent calls for the same
// (project, resolved-image) are serialized via singleflight.
// opts are applied only when a new container is created; a cached container ignores them.
func (m *Manager) EnsureInstance(ctx context.Context, projectPath, configImage string, opts sandbox.StartOptions) (*sandbox.Instance, error) {
	// Resolve the image here to compute the correct singleflight key.
	image := resolveImage(projectPath, configImage)
	key := containerKey(projectPath, image)
	_, err, _ := m.inflight.Do(key, func() (any, error) {
		return nil, m.ensureContainer(ctx, projectPath, configImage, opts)
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	cs := m.containers[key]
	m.mu.Unlock()

	return &sandbox.Instance{
		ProjectPath: projectPath,
		Image:       image,
		Internal:    cs,
	}, nil
}

func (m *Manager) ensureContainer(ctx context.Context, projectPath, configImage string, opts sandbox.StartOptions) error {
	// Resolve final image (devcontainer.json > config > default).
	image := resolveImage(projectPath, configImage)
	name := containerName(projectPath, image)
	key := containerKey(projectPath, image)

	m.mu.Lock()
	_, exists := m.containers[key]
	m.mu.Unlock()

	if exists {
		return nil
	}

	if running, _ := isContainerRunning(ctx, name); running {
		// Daemon restarted; container from prior session is still alive — reclaim it.
		slog.Info("docker sandbox: reclaiming existing container", "name", name, "project", projectPath, "image", image)
		m.mu.Lock()
		m.containers[key] = &containerState{name: name, image: image}
		m.mu.Unlock()
		return nil
	}

	if err := m.startContainer(ctx, projectPath, image, name, opts); err != nil {
		return err
	}

	m.mu.Lock()
	m.containers[key] = &containerState{name: name, image: image}
	m.mu.Unlock()
	return nil
}

func baseContainerArgs(cfg Config, projectPath, image, name string) []string {
	home, _ := os.UserHomeDir()
	roostSock := filepath.Join(home, ".roost", "roost.sock")
	uid := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"--label", "roost-managed=1",
		"--label", "roost-project=" + projectPath,
		"--label", "roost-image=" + image,
		"--user", uid,
		"-v", projectPath + ":" + projectPath + ":rw",
		"-v", roostSock + ":/tmp/roost.sock:rw",
		"-e", "ROOST_SOCKET=/tmp/roost.sock", "-e", "HOME=/home/user",
		"--network", cfg.Network, "--cap-drop=ALL", "--security-opt=no-new-privileges",
	}
	return append(args, cfg.ExtraArgs...)
}

func (m *Manager) startContainer(ctx context.Context, projectPath, image, name string, opts sandbox.StartOptions) error {
	if err := ensureNetwork(ctx, m.cfg.Network); err != nil {
		return fmt.Errorf("docker sandbox: ensure network %s: %w", m.cfg.Network, err)
	}
	args := baseContainerArgs(m.cfg, projectPath, image, name)

	for _, mount := range opts.ExtraMounts {
		hostPath := expandPath(strings.SplitN(mount, ":", 2)[0])
		if _, err := os.Stat(hostPath); err != nil {
			slog.Warn("docker sandbox: skipping mount (host path absent)", "mount", mount)
			continue
		}
		args = append(args, "-v", expandMountSpec(mount))
	}
	for k, v := range opts.Env {
		args = append(args, "-e", shellEscape(k+"="+v))
	}
	for _, key := range opts.ForwardEnv {
		if v, ok := os.LookupEnv(key); ok {
			args = append(args, "-e", shellEscape(key+"="+v))
		}
	}
	args = append(args, image, "sleep", "infinity")

	slog.Info("docker sandbox: starting container", "name", name, "image", image, "project", projectPath)
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s: %w\n%s", name, err, out)
	}
	return nil
}

// BuildLaunchCommand generates a "docker exec" command to run plan inside inst.
func (m *Manager) BuildLaunchCommand(inst *sandbox.Instance, plan state.LaunchPlan, env map[string]string) (string, map[string]string, error) {
	cs, ok := inst.Internal.(*containerState)
	if !ok {
		return "", nil, fmt.Errorf("docker sandbox: invalid instance for %s", inst.ProjectPath)
	}

	workDir := plan.StartDir
	if workDir == "" {
		workDir = inst.ProjectPath
	}

	// "shell" is a roost-internal keyword that means "spawn a login shell".
	// Translate it to bash (with sh fallback) for the docker exec context.
	command := plan.Command
	if command == "shell" {
		command = "/bin/bash"
	}

	// Build "docker exec" command with env vars passed via -e flags.
	var sb strings.Builder
	sb.WriteString("docker exec -it")
	sb.WriteString(" -w ")
	sb.WriteString(shellEscape(workDir))
	for k, v := range env {
		sb.WriteString(" -e ")
		sb.WriteString(shellEscape(k + "=" + v))
	}
	sb.WriteString(" ")
	sb.WriteString(cs.name)
	sb.WriteString(" ")
	sb.WriteString(command)

	// Env is passed via -e flags; return empty map so the runtime does not
	// inject them as tmux window env (they'd be double-set).
	return sb.String(), map[string]string{}, nil
}

// AcquireFrame increments the ref-count for the instance.
func (m *Manager) AcquireFrame(inst *sandbox.Instance) {
	cs := inst.Internal.(*containerState) //nolint:forcetypeassert
	cs.mu.Lock()
	cs.refCount++
	cs.mu.Unlock()
}

// ReleaseFrame decrements the ref-count. Returns true when the container
// should be destroyed (count reached zero).
func (m *Manager) ReleaseFrame(inst *sandbox.Instance) bool {
	cs := inst.Internal.(*containerState) //nolint:forcetypeassert
	cs.mu.Lock()
	cs.refCount--
	zero := cs.refCount <= 0
	cs.mu.Unlock()
	return zero
}

// DestroyInstance kills the Docker container and removes it from the registry.
func (m *Manager) DestroyInstance(ctx context.Context, inst *sandbox.Instance) error {
	cs := inst.Internal.(*containerState) //nolint:forcetypeassert
	m.mu.Lock()
	delete(m.containers, containerKey(inst.ProjectPath, inst.Image))
	m.mu.Unlock()

	slog.Info("docker sandbox: stopping container", "name", cs.name, "project", inst.ProjectPath)
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(stopCtx, "docker", "kill", cs.name).CombinedOutput()
	if err != nil {
		slog.Warn("docker sandbox: kill failed", "name", cs.name, "err", err, "out", string(out))
		return fmt.Errorf("docker kill %s: %w", cs.name, err)
	}
	return nil
}

// PruneOrphans stops roost-managed containers that are not associated with any
// known project, or whose image no longer matches what resolveImage returns.
// Call once at startup after loading the session snapshot.
func (m *Manager) PruneOrphans(ctx context.Context, knownProjects []string, resolveImage func(string) string) {
	known := make(map[string]struct{}, len(knownProjects))
	for _, p := range knownProjects {
		known[p] = struct{}{}
	}

	// List all roost-managed container names.
	listOut, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=roost-managed=1",
		"--format", "{{.Names}}").Output()
	if err != nil {
		slog.Warn("docker sandbox: list managed containers failed", "err", err)
		return
	}
	names := strings.Fields(string(listOut))
	if len(names) == 0 {
		return
	}

	// Batch-inspect to read roost-project and roost-image labels.
	inspectFmt := `{{.Name}}` + "\t" + `{{index .Config.Labels "roost-project"}}` + "\t" + `{{index .Config.Labels "roost-image"}}`
	inspectArgs := append([]string{"inspect", "--format", inspectFmt}, names...)
	inspectOut, err := exec.CommandContext(ctx, "docker", inspectArgs...).Output()
	if err != nil {
		slog.Warn("docker sandbox: inspect failed during prune", "err", err)
		return
	}

	for _, line := range strings.Split(strings.TrimSpace(string(inspectOut)), "\n") {
		parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(line), "/"), "\t", 3)
		if len(parts) != 3 {
			continue
		}
		cName, projectPath, labelImage := parts[0], parts[1], parts[2]
		if _, ok := known[projectPath]; ok {
			// Project is known. Also verify the image is still current.
			if labelImage != "" && resolveImage != nil && resolveImage(projectPath) == labelImage {
				continue // project known + image matches → keep
			}
		}
		slog.Info("docker sandbox: pruning orphan container", "name", cName, "project", projectPath, "label_image", labelImage)
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if _, killErr := exec.CommandContext(stopCtx, "docker", "kill", cName).CombinedOutput(); killErr != nil {
			slog.Warn("docker sandbox: orphan prune failed", "name", cName, "err", killErr)
		}
		cancel()
	}
}

// containerName derives a stable Docker container name from the (project, image) pair.
// Format: roost-<8-char sha256 prefix>. Different images for the same project
// produce distinct names, allowing multiple containers per project.
func containerName(projectPath, image string) string {
	h := sha256.Sum256([]byte(projectPath + "\n" + image))
	return fmt.Sprintf("roost-%x", h[:4])
}

// expandPath replaces a leading "~" with the user home directory.
func expandPath(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// expandMountSpec expands "~" in the host portion of a "host:guest[:mode]" mount spec.
func expandMountSpec(spec string) string {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) < 2 {
		return spec
	}
	return expandPath(parts[0]) + ":" + parts[1]
}

// isContainerRunning checks whether a container with name is currently running.
func isContainerRunning(ctx context.Context, name string) (bool, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-q", "--filter", "name=^/"+name+"$").Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// ensureNetwork creates the docker network if it does not already exist.
// Concurrent calls are safe: "already exists" errors are silently ignored.
func ensureNetwork(ctx context.Context, name string) error {
	out, err := exec.CommandContext(ctx, "docker", "network", "create", name).CombinedOutput()
	if err != nil {
		// "already exists" is not an error; any other failure is.
		if strings.Contains(string(out), "already exists") {
			return nil
		}
		return fmt.Errorf("docker network create %s: %w\n%s", name, err, string(out))
	}
	slog.Info("docker sandbox: created network", "network", name)
	return nil
}

// shellEscape wraps s in single quotes with inner single-quote escaping.
func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
