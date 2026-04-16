package config

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

// workspaceCacheEntry caches the resolved workspace name for one project path.
type workspaceCacheEntry struct {
	workspace    string
	settingsPath string // "" when no .roost/settings.toml was found
	mtime        time.Time
}

// WorkspaceResolver resolves the workspace name for a project directory,
// caching results by the settings file's mtime to avoid repeated TOML reads.
// It is safe for concurrent use.
type WorkspaceResolver struct {
	mu    sync.Mutex
	cache map[string]workspaceCacheEntry
}

// NewWorkspaceResolver creates a ready-to-use WorkspaceResolver.
func NewWorkspaceResolver() *WorkspaceResolver {
	return &WorkspaceResolver{
		cache: make(map[string]workspaceCacheEntry),
	}
}

// Resolve returns the workspace name for the project rooted at projectPath.
// It always returns a non-empty string — projects with no settings or with an
// invalid workspace name fall back to DefaultWorkspaceName.
func (r *WorkspaceResolver) Resolve(projectPath string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.cache[projectPath]; ok {
		if entry.settingsPath == "" {
			// Previously found no settings file; return cached default.
			return DefaultWorkspaceName
		}
		info, err := os.Stat(entry.settingsPath)
		if err == nil && info.ModTime().Equal(entry.mtime) {
			return entry.workspace
		}
		// File changed or disappeared; fall through to re-read.
	}

	settingsPath := findProjectSettings(projectPath)
	if settingsPath == "" {
		r.cache[projectPath] = workspaceCacheEntry{workspace: DefaultWorkspaceName}
		return DefaultWorkspaceName
	}

	info, statErr := os.Stat(settingsPath)
	cfg, err := LoadProjectFrom(settingsPath)
	ws := DefaultWorkspaceName
	if err != nil {
		slog.Debug("workspace: failed to load project settings", "path", settingsPath, "err", err)
	} else if validateErr := cfg.Validate(); validateErr != nil {
		slog.Debug("workspace: invalid project settings", "path", settingsPath, "err", validateErr)
	} else {
		ws = cfg.WorkspaceName()
	}

	var mtime time.Time
	if statErr == nil {
		mtime = info.ModTime()
	}
	r.cache[projectPath] = workspaceCacheEntry{
		workspace:    ws,
		settingsPath: settingsPath,
		mtime:        mtime,
	}
	return ws
}

// Invalidate clears all cached entries, forcing the next Resolve call to
// re-read the settings files.
func (r *WorkspaceResolver) Invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]workspaceCacheEntry)
}
