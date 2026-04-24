package config

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

type sandboxCacheEntry struct {
	resolved     SandboxConfig
	settingsPath string // "" when no .roost/settings.toml was found
	mtime        time.Time
}

// SandboxResolver resolves the effective SandboxConfig for a project directory
// by merging user-scope and project-scope settings. Results are cached by the
// settings file's mtime. Safe for concurrent use.
type SandboxResolver struct {
	user  SandboxConfig
	mu    sync.Mutex
	cache map[string]sandboxCacheEntry
}

// NewSandboxResolver creates a resolver with the given user-scope sandbox config.
func NewSandboxResolver(user SandboxConfig) *SandboxResolver {
	return &SandboxResolver{
		user:  user,
		cache: make(map[string]sandboxCacheEntry),
	}
}

// Resolve returns the effective SandboxConfig for projectPath, merging user
// and project scopes. An empty projectPath or absent settings file returns the
// user config unchanged. Parse errors fall back to user config with a warning.
func (r *SandboxResolver) Resolve(projectPath string) SandboxConfig {
	if projectPath == "" {
		return r.user
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.cache[projectPath]; ok {
		if entry.settingsPath == "" {
			return r.user
		}
		info, err := os.Stat(entry.settingsPath)
		if err == nil && info.ModTime().Equal(entry.mtime) {
			return entry.resolved
		}
	}

	settingsPath := findProjectSettings(projectPath)
	if settingsPath == "" {
		r.cache[projectPath] = sandboxCacheEntry{resolved: r.user}
		return r.user
	}

	info, statErr := os.Stat(settingsPath)
	proj, err := LoadProjectFrom(settingsPath)
	resolved := r.user
	if err != nil {
		slog.Warn("sandbox resolver: failed to load project settings", "path", settingsPath, "err", err)
	} else {
		resolved = MergeSandbox(r.user, proj.Sandbox)
	}

	var mtime time.Time
	if statErr == nil {
		mtime = info.ModTime()
	}
	r.cache[projectPath] = sandboxCacheEntry{
		resolved:     resolved,
		settingsPath: settingsPath,
		mtime:        mtime,
	}
	return resolved
}
