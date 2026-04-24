// Package sandbox defines the backend-agnostic interface for project-level
// sandbox lifecycle management (Docker, Firecracker, …).
//
// Each backend creates one long-lived sandbox instance per project directory.
// Frames (tmux windows / docker exec sessions) join by calling AcquireFrame;
// the sandbox is destroyed when the last frame calls ReleaseFrame.
package sandbox

import (
	"context"

	"github.com/takezoh/agent-roost/state"
)

// Instance represents a running sandbox for one project directory.
// The concrete fields are managed by the backend; callers treat this as opaque.
type Instance struct {
	ProjectPath string // canonical absolute path
	Image       string // docker image (or equivalent) used to start this instance
	// Internal fields are embedded by backend implementations via composition.
	Internal any
}

// StartOptions carries optional per-launch parameters for starting a new sandbox
// instance. Options are only applied when the instance is freshly created; a
// cached (running) instance ignores them.
type StartOptions struct {
	ExtraMounts []string          // additional bind-mount specs, "host:guest[:mode]"
	Env         map[string]string // fixed env vars to set in the container
	ForwardEnv  []string          // host env var names to pass through if set on the host
}

// Manager is the backend-neutral lifecycle controller for project sandboxes.
// Implementations must be safe for concurrent use from multiple goroutines.
type Manager interface {
	// EnsureInstance starts the sandbox for the (projectPath, image) pair if not
	// already running, or returns the existing instance. opts only apply when a
	// new instance is created. Concurrent calls for the same (project, image) must
	// be serialized (e.g. via singleflight).
	EnsureInstance(ctx context.Context, projectPath, image string, opts StartOptions) (*Instance, error)

	// BuildLaunchCommand generates the shell command string and environment to
	// run plan inside the sandbox instance. The returned command is passed to
	// TmuxBackend.SpawnWindow.
	BuildLaunchCommand(inst *Instance, plan state.LaunchPlan, env map[string]string) (command string, outEnv map[string]string, err error)

	// AcquireFrame increments the ref-count for the instance.
	// Must be called before the frame is spawned.
	AcquireFrame(inst *Instance)

	// ReleaseFrame decrements the ref-count. Returns true when the count
	// drops to zero — the caller should then call DestroyInstance.
	ReleaseFrame(inst *Instance) bool

	// DestroyInstance stops and removes the sandbox. Only called when
	// ReleaseFrame returns true (ref-count == 0).
	DestroyInstance(ctx context.Context, inst *Instance) error

	// PruneOrphans stops sandbox instances that are not associated with any
	// of knownProjects, or whose resolved image no longer matches what
	// resolveImage returns for the project (e.g. after a config change).
	// Call once at startup to clean up leftovers from a prior daemon run.
	// knownProjects is the set of canonical project paths from the snapshot.
	// resolveImage maps a project path to the currently-effective image; a
	// container whose image label differs from this value is also pruned.
	PruneOrphans(ctx context.Context, knownProjects []string, resolveImage func(string) string)
}
