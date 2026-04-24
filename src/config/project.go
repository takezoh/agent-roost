package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

// ProjectConfig holds the contents of a project-level .roost/settings.toml.
// All fields have zero values that represent sensible defaults, so a missing
// file is not an error.
type ProjectConfig struct {
	Workspace ProjectWorkspaceConfig `toml:"workspace"`
	// Sandbox, when non-nil, overrides the user-scope sandbox config for this project.
	// Only fields explicitly set in the project file are meaningful; missing scalars
	// are empty strings and should be treated as "no override" in merge logic.
	Sandbox *SandboxConfig `toml:"sandbox"`
}

// ProjectWorkspaceConfig is the [workspace] table inside a project settings
// file.
type ProjectWorkspaceConfig struct {
	Name string `toml:"name"`
}

// DefaultWorkspaceName is the workspace assigned to any project that does not
// explicitly set one.
const DefaultWorkspaceName = "default"

// MaxWorkspaceNameLen is the maximum rune-length of a workspace name.
const MaxWorkspaceNameLen = 64

// LoadProjectFrom reads the file at path as a project-level settings.toml.
// A missing file is not an error; it returns a zero-value *ProjectConfig.
// Parse errors are returned as-is.
func LoadProjectFrom(path string) (*ProjectConfig, error) {
	cfg := &ProjectConfig{}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadProject resolves the project settings file for the given project
// directory by walking up the filesystem until it finds a .roost/settings.toml
// or reaches the root. A non-existing file returns a zero-value *ProjectConfig.
func LoadProject(projectDir string) (*ProjectConfig, error) {
	path := findProjectSettings(projectDir)
	if path == "" {
		return &ProjectConfig{}, nil
	}
	return LoadProjectFrom(path)
}

// WorkspaceName returns the configured workspace name, or DefaultWorkspaceName
// when the name is empty or contains only whitespace.
func (pc *ProjectConfig) WorkspaceName() string {
	name := strings.TrimSpace(pc.Workspace.Name)
	if name == "" {
		return DefaultWorkspaceName
	}
	return name
}

// Validate reports an error when the workspace name is invalid. An empty name
// (meaning "use default") is always valid. Invalid conditions:
//   - contains ASCII control characters
//   - exceeds MaxWorkspaceNameLen runes
func (pc *ProjectConfig) Validate() error {
	name := strings.TrimSpace(pc.Workspace.Name)
	if name == "" {
		return nil
	}
	runes := []rune(name)
	if len(runes) > MaxWorkspaceNameLen {
		return fmt.Errorf("workspace.name: too long (%d runes, max %d)", len(runes), MaxWorkspaceNameLen)
	}
	for _, r := range runes {
		if unicode.IsControl(r) {
			return fmt.Errorf("workspace.name: contains control character %U", r)
		}
	}
	return nil
}

// findProjectSettings walks up from dir searching for .roost/settings.toml.
// Returns the absolute path of the first match, or "" if none found.
// This is intentionally a pure filesystem walk — it does not shell out to git —
// so the config package stays free of lib/git dependency.
func findProjectSettings(dir string) string {
	dir = filepath.Clean(dir)
	for {
		candidate := filepath.Join(dir, ".roost", "settings.toml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
