package tui

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
	Client  TmuxRunner
	Manager SessionRunner
	Config  ToolConfig
	Args    map[string]string
}

type TmuxRunner interface {
	SetEnv(key, value string) error
	DetachClient() error
	KillSession() error
	SendKeys(target, keys string) error
	NewWindow(name, command, startDir string) (string, error)
	SetOption(target, key, value string) error
}

type SessionRunner interface {
	Create(project, command string) error
	Stop(sessionID string) error
}

type ToolConfig struct {
	SessionName    string
	DefaultCommand string
	Commands       []string
	Projects       []string
}

type Registry struct {
	tools   []Tool
	byName  map[string]*Tool
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
			return ctx.Manager.Create(args["project"], args["command"])
		},
	})
	r.Register(Tool{
		Name:        "add-project",
		Description: "プロジェクトを一覧に追加",
		Params: []Param{
			{Name: "project", Options: func(ctx *ToolContext) []string { return ctx.Config.Projects }},
		},
		Run: func(ctx *ToolContext, args map[string]string) error {
			return nil // Model 側で projects map に追加
		},
	})
	r.Register(Tool{
		Name:        "stop-session",
		Description: "セッションを停止",
		Params: []Param{
			{Name: "session_id", Options: func(ctx *ToolContext) []string { return nil }},
		},
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Manager.Stop(args["session_id"])
		},
	})
	r.Register(Tool{
		Name:        "detach",
		Description: "デタッチ（セッション維持）",
		Run: func(ctx *ToolContext, args map[string]string) error {
			return ctx.Client.DetachClient()
		},
	})
	r.Register(Tool{
		Name:        "quit",
		Description: "全終了（セッション破棄）",
		Run: func(ctx *ToolContext, args map[string]string) error {
			ctx.Client.SetEnv("ROOST_SHUTDOWN", "1")
			return ctx.Client.DetachClient()
		},
	})
	return r
}

func ProjectDisplayName(path string) string {
	return filepath.Base(path)
}
