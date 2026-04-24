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
	// Internal fields are embedded by backend implementations via composition.
	Internal any
}

// Manager is the backend-neutral lifecycle controller for project sandboxes.
// Implementations must be safe for concurrent use from multiple goroutines.
type Manager interface {
	// EnsureInstance starts the sandbox for projectPath if not already running,
	// or returns the existing instance. Concurrent calls for the same project
	// must be serialized (e.g. via singleflight) to prevent duplicate sandboxes.
	EnsureInstance(ctx context.Context, projectPath string) (*Instance, error)

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

	// Shutdown stops all sandbox instances at daemon exit.
	Shutdown(ctx context.Context) error

	// PruneOrphans stops sandbox instances that are not associated with any
	// of knownProjects. Call once at startup to clean up leftovers from a
	// prior daemon run. knownProjects is the set of canonical project paths
	// loaded from the session snapshot.
	PruneOrphans(ctx context.Context, knownProjects []string)
}
