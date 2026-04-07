package core

import (
	"path/filepath"
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
	Run         func(ctx *ToolContext, args map[string]string) error
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
		Run: func(ctx *ToolContext, args map[string]string) error {
			_, err := ctx.Client.CreateSession(args["project"], args["command"])
			return err
		},
	})
	r.Register(Tool{
		Name:        "stop-session",
		Description: "Stop session",
		Params: []Param{
			{Name: "session_id", Options: func(ctx *ToolContext) []string { return nil }},
		},
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.StopSession(args["session_id"])
		},
	})
	r.Register(Tool{
		Name:        "detach",
		Description: "Detach (keep session)",
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.Detach()
		},
	})
	r.Register(Tool{
		Name:        "shutdown",
		Description: "Shutdown (discard sessions)",
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.Shutdown()
		},
	})
	return r
}

func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}
