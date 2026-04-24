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
type Config struct {
	Image     string   // user-specified image; empty → auto-resolve
	Network   string   // docker network name; empty → "roost-sandbox"
	ExtraArgs []string // extra args appended to "docker run"
}

// containerState holds the runtime data for one project's container.
type containerState struct {
	mu       sync.Mutex
	name     string // docker container name
	refCount int
}

// Manager is the Docker implementation of sandbox.Manager.
type Manager struct {
	cfg      Config
	mu       sync.Mutex
	inflight singleflight.Group
	// containers maps canonical project path → container state
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

// EnsureInstance returns the running container for projectPath, starting one
// if necessary. Concurrent calls for the same project are serialized via
// singleflight so only one "docker run" fires per project.
func (m *Manager) EnsureInstance(ctx context.Context, projectPath string) (*sandbox.Instance, error) {
	_, err, _ := m.inflight.Do(projectPath, func() (any, error) {
		return nil, m.ensureContainer(ctx, projectPath)
	})
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	cs := m.containers[projectPath]
	m.mu.Unlock()

	return &sandbox.Instance{
		ProjectPath: projectPath,
		Internal:    cs,
	}, nil
}

func (m *Manager) ensureContainer(ctx context.Context, projectPath string) error {
	name := containerName(projectPath)

	m.mu.Lock()
	_, exists := m.containers[projectPath]
	m.mu.Unlock()

	if exists {
		return nil
	}

	if running, _ := isContainerRunning(ctx, name); running {
		// Daemon restarted; container from prior session is still alive — reclaim it.
		slog.Info("docker sandbox: reclaiming existing container", "name", name, "project", projectPath)
		m.mu.Lock()
		m.containers[projectPath] = &containerState{name: name}
		m.mu.Unlock()
		return nil
	}

	if err := m.startContainer(ctx, projectPath, name); err != nil {
		return err
	}

	m.mu.Lock()
	m.containers[projectPath] = &containerState{name: name}
	m.mu.Unlock()
	return nil
}

func (m *Manager) startContainer(ctx context.Context, projectPath, name string) error {
	if err := ensureNetwork(ctx, m.cfg.Network); err != nil {
		return fmt.Errorf("docker sandbox: ensure network %s: %w", m.cfg.Network, err)
	}

	image := resolveImage(projectPath, m.cfg.Image)

	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")
	roostSock := filepath.Join(home, ".roost", "roost.sock")

	uid := fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())

	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"--label", "roost-managed=1",
		"--user", uid,
		"-v", projectPath + ":" + projectPath + ":rw",
		"-v", claudeDir + ":/home/user/.claude:ro",
		"-v", roostSock + ":/tmp/roost.sock:rw",
		"-e", "ROOST_SOCKET=/tmp/roost.sock",
		"--network", m.cfg.Network,
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
	}
	args = append(args, m.cfg.ExtraArgs...)
	args = append(args, image, "sleep", "infinity")

	slog.Info("docker sandbox: starting container", "name", name, "image", image, "project", projectPath)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
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
	delete(m.containers, inst.ProjectPath)
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

// Shutdown is intentionally a no-op: containers must survive daemon shutdown
// so that tmux panes running "docker exec" stay alive for warm-restart adoption.
// Containers are destroyed only when frames are explicitly killed (DestroyInstance
// via ReleaseFrame ref-count == 0). Orphans from prior crashed daemons are cleaned
// up at next startup by PruneOrphans.
func (m *Manager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	m.containers = map[string]*containerState{}
	m.mu.Unlock()
	return nil
}

// PruneOrphans stops roost-managed containers that are not associated with any
// known project. Call once at startup (after loading the session snapshot) to
// clean up containers left over from a prior daemon run that was never
// restarted. knownProjects is the set of canonical project paths from the snapshot.
func (m *Manager) PruneOrphans(ctx context.Context, knownProjects []string) {
	known := make(map[string]struct{}, len(knownProjects))
	for _, p := range knownProjects {
		known[containerName(p)] = struct{}{}
	}

	out, err := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=roost-managed=1",
		"--format", "{{.Names}}").Output()
	if err != nil {
		slog.Warn("docker sandbox: list managed containers failed", "err", err)
		return
	}

	for _, name := range strings.Fields(string(out)) {
		if _, ok := known[name]; ok {
			continue
		}
		slog.Info("docker sandbox: pruning orphan container", "name", name)
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if _, killErr := exec.CommandContext(stopCtx, "docker", "kill", name).CombinedOutput(); killErr != nil {
			slog.Warn("docker sandbox: orphan prune failed", "name", name, "err", killErr)
		}
		cancel()
	}
}

// containerName derives a stable Docker container name from the project path.
// Format: roost-<8-char sha256 prefix of path>.
func containerName(projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	return fmt.Sprintf("roost-%x", h[:4])
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
