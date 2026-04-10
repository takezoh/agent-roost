// Package tools is the user-facing palette tool registry. Each tool
// describes a high-level operation (create session, stop session,
// detach, ...) the palette can drive. Tools are decoupled from the
// daemon by going through proto.Client — the same interface the TUI
// processes use to talk to the daemon.
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/take/agent-roost/proto"
)

// Param describes one input the palette must collect before running
// a tool. Options is called at runtime to populate the picker.
type Param struct {
	Name    string
	Options func(ctx *ToolContext) []string
}

// Tool is one user-facing palette command.
type Tool struct {
	Name        string
	Description string
	Params      []Param
	Run         func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error)
}

// ToolInvocation tells the palette to immediately start another tool
// in the same popup process after the current Run returns. Used for
// in-popup tool chains (e.g. create-project → new-session) since
// tmux disallows nested display-popups.
type ToolInvocation struct {
	Name string
	Args map[string]string
}

// ToolContext is the bag of dependencies handed to every Tool.Run
// call. Client is the daemon connection; Config carries static
// palette config (commands, projects).
type ToolContext struct {
	Client *proto.Client
	Config ToolConfig
	Args   map[string]string
}

type ToolConfig struct {
	DefaultCommand string
	Commands       []string
	Projects       []string
	ProjectRoots   []string
}

// Registry holds the tools available to the palette.
type Registry struct {
	tools  []Tool
	byName map[string]*Tool
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*Tool)}
}

func (r *Registry) Register(t Tool) {
	r.tools = append(r.tools, t)
	r.byName[t.Name] = &r.tools[len(r.tools)-1]
}

func (r *Registry) Get(name string) *Tool { return r.byName[name] }

func (r *Registry) All() []Tool { return r.tools }

func (r *Registry) Match(query string) []Tool {
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

// DefaultRegistry returns the built-in palette tool set.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(Tool{
		Name:        "new-session",
		Description: "Create session",
		Params: []Param{
			{Name: "project", Options: func(ctx *ToolContext) []string { return ctx.Config.Projects }},
			{Name: "command", Options: func(ctx *ToolContext) []string { return ctx.Config.Commands }},
		},
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			_, _, err := ctx.Client.CreateSession(args["project"], args["command"])
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

// ProjectDisplayName returns the human-readable label for a project
// path (currently just basename).
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

// makeProjectDir creates a new project directory `name` under `root`.
// `root` must be one of the configured project_roots — palette
// free-form input fallback (when ProjectRoots is empty) must not be
// allowed to create directories at arbitrary paths. The name is
// validated to forbid path separators (`/`, `\`) and leading dots.
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
