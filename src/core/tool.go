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

func (r *Registry) Get(name string) *Tool {
	return r.byName[name]
}

func (r *Registry) All() []Tool {
	return r.tools
}

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

func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(Tool{
		Name:        "new-session",
		Description: "セッション作成",
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
		Description: "セッションを停止",
		Params: []Param{
			{Name: "session_id", Options: func(ctx *ToolContext) []string { return nil }},
		},
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.StopSession(args["session_id"])
		},
	})
	r.Register(Tool{
		Name:        "detach",
		Description: "デタッチ（セッション維持）",
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.Detach()
		},
	})
	r.Register(Tool{
		Name:        "shutdown",
		Description: "全終了（セッション破棄）",
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.Shutdown()
		},
	})
	return r
}

func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}
