// Package tools is the user-facing palette tool registry. Each tool
// describes a high-level operation (create session, stop session,
// detach, ...) the palette can drive. Tools are decoupled from the
// daemon by going through proto.Client — the same interface the TUI
// processes use to talk to the daemon.
package tools

import (
	"path/filepath"
	"strings"

	"github.com/takezoh/agent-roost/proto"
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
	Hidden      bool // Hidden tools are excluded from All() and Match() but reachable via Get()
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
	Client       *proto.Client
	Config       ToolConfig
	Args         map[string]string
	IsGitProject func(path string) bool // nil-safe; injected by main
}

type ToolConfig struct {
	DefaultCommand string
	Commands       []string
	PushCommands   []string
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

func (r *Registry) All() []Tool {
	var visible []Tool
	for _, t := range r.tools {
		if !t.Hidden {
			visible = append(visible, t)
		}
	}
	return visible
}

func (r *Registry) Match(query string) []Tool {
	q := strings.ToLower(query)
	var matched []Tool
	for _, t := range r.tools {
		if t.Hidden {
			continue
		}
		if q == "" || strings.Contains(strings.ToLower(t.Name), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			matched = append(matched, t)
		}
	}
	return matched
}

// ProjectDisplayName returns the human-readable label for a project
// path (currently just basename).
func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}
