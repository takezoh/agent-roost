package core

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Param struct {
	Name    string
	Options func(ctx *ToolContext) []string
}

type Tool struct {
	Name        string
	Description string
	Params      []Param
	Run         func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error)
}

// ToolInvocation tells the palette to immediately start another tool in the
// same popup process after the current Run returns. Used for in-popup tool
// chains (e.g. create-project → new-session). tmux does not allow nesting
// display-popup, so chains must transition the tea.Model rather than asking
// the daemon to spawn a new popup.
type ToolInvocation struct {
	Name string
	Args map[string]string
}

type ToolContext struct {
	Client *Client
	Config ToolConfig
	Args   map[string]string
}

type ToolConfig struct {
	DefaultCommand string
	Commands       []string
	Projects       []string
	ProjectRoots   []string
}

type ToolRegistry struct {
	tools  []Tool
	byName map[string]*Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{byName: make(map[string]*Tool)}
}

func (r *ToolRegistry) Register(t Tool) {
	r.tools = append(r.tools, t)
	r.byName[t.Name] = &r.tools[len(r.tools)-1]
}

func (r *ToolRegistry) Get(name string) *Tool {
	return r.byName[name]
}

func (r *ToolRegistry) All() []Tool {
	return r.tools
}

func (r *ToolRegistry) Match(query string) []Tool {
	if query == "" {
		return r.tools
	}
	q := strings.ToLower(query)
	var matched []Tool
	for _, t := range r.tools {
		if strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			matched = append(matched, t)
		}
	}
	return matched
}

func DefaultToolRegistry() *ToolRegistry {
	r := NewToolRegistry()
	r.Register(Tool{
		Name:        "new-session",
		Description: "Create session",
		Params: []Param{
			{Name: "project", Options: func(ctx *ToolContext) []string { return ctx.Config.Projects }},
			{Name: "command", Options: func(ctx *ToolContext) []string { return ctx.Config.Commands }},
		},
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			_, err := ctx.Client.CreateSession(args["project"], args["command"])
			return nil, err
		},
	})
	r.Register(Tool{
		Name:        "create-project",
		Description: "Create new project dir and start session",
		Params: []Param{
			{Name: "root", Options: func(ctx *ToolContext) []string { return ctx.Config.ProjectRoots }},
			{Name: "name", Options: func(ctx *ToolContext) []string { return nil }},
		},
		Run: runCreateProject,
	})
	r.Register(Tool{
		Name:        "stop-session",
		Description: "Stop session",
		Params: []Param{
			{Name: "session_id", Options: func(ctx *ToolContext) []string { return nil }},
		},
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			return nil, ctx.Client.StopSession(args["session_id"])
		},
	})
	r.Register(Tool{
		Name:        "detach",
		Description: "Detach (keep session)",
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			return nil, ctx.Client.Detach()
		},
	})
	r.Register(Tool{
		Name:        "shutdown",
		Description: "Shutdown (discard sessions)",
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			return nil, ctx.Client.Shutdown()
		},
	})
	return r
}

func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}

func runCreateProject(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
	path, err := makeProjectDir(ctx.Config.ProjectRoots, args["root"], args["name"])
	if err != nil {
		return nil, err
	}
	return &ToolInvocation{
		Name: "new-session",
		Args: map[string]string{"project": path},
	}, nil
}

// makeProjectDir creates a new project directory `name` under `root`. `root`
// must be one of the configured project_roots — palette free-form input
// fallback (when ProjectRoots is empty) must not be allowed to create
// directories at arbitrary paths. The name is validated to forbid path
// separators (`/`, `\`) and leading dots (hidden files, `.`, `..`).
func makeProjectDir(roots []string, root, name string) (string, error) {
	if !slices.Contains(roots, root) {
		return "", fmt.Errorf("root must be one of configured project_roots")
	}
	if name == "" {
		return "", fmt.Errorf("name required")
	}
	if strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("invalid project name: %q", name)
	}
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}
