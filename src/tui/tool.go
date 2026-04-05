package tui

import "strings"

type Tool struct {
	Name        string
	Description string
	Run         func(ctx *ToolContext) error
}

type ToolContext struct {
	Client  TmuxRunner
	Manager SessionRunner
	Config  ToolConfig
}

type TmuxRunner interface {
	SetEnv(key, value string) error
	DetachClient() error
	KillSession() error
	SendKeys(target, keys string) error
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
	tools []Tool
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(t Tool) {
	r.tools = append(r.tools, t)
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
		Description: "デフォルトコマンドでセッション作成",
		Run: func(ctx *ToolContext) error {
			ctx.Client.SendKeys(ctx.Config.SessionName+":0.2", "n")
			return nil
		},
	})
	r.Register(Tool{
		Name:        "new-with-command",
		Description: "コマンドを選択してセッション作成",
		Run: func(ctx *ToolContext) error {
			ctx.Client.SendKeys(ctx.Config.SessionName+":0.2", "N")
			return nil
		},
	})
	r.Register(Tool{
		Name:        "add-project",
		Description: "プロジェクトを一覧に追加",
		Run: func(ctx *ToolContext) error {
			ctx.Client.SendKeys(ctx.Config.SessionName+":0.2", "p")
			return nil
		},
	})
	r.Register(Tool{
		Name:        "stop-session",
		Description: "セッションを停止",
		Run: func(ctx *ToolContext) error {
			ctx.Client.SendKeys(ctx.Config.SessionName+":0.2", "d")
			return nil
		},
	})
	r.Register(Tool{
		Name:        "detach",
		Description: "デタッチ（セッション維持）",
		Run: func(ctx *ToolContext) error {
			return ctx.Client.DetachClient()
		},
	})
	r.Register(Tool{
		Name:        "quit",
		Description: "全終了（セッション破棄）",
		Run: func(ctx *ToolContext) error {
			ctx.Client.SetEnv("ROOST_SHUTDOWN", "1")
			return ctx.Client.DetachClient()
		},
	})
	return r
}
