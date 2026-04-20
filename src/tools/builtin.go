package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/takezoh/agent-roost/features"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// DefaultRegistry returns the built-in palette tool set.
// feats gates optional tools behind runtime feature flags.
func DefaultRegistry(feats features.Set) *Registry { //nolint:funlen
	r := NewRegistry()
	r.Register(Tool{
		Name:        "new-session",
		Description: "Create session",
		Params: []Param{
			{Name: "project", Options: func(ctx *ToolContext) []string { return ctx.Config.Projects }},
			{Name: "command", Options: func(ctx *ToolContext) []string { return ctx.Config.Commands }},
		},
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			_, err := ctx.Client.CreateSession(args["project"], args["command"], state.LaunchOptions{
				Worktree: state.WorktreeOption{Enabled: args["worktree"] == "on"},
			})
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
	r.Register(Tool{
		Name:        "push-driver",
		Description: "Push driver onto active session",
		Hidden:      true,
		Params: []Param{
			{Name: "command", Options: func(ctx *ToolContext) []string { return ctx.Config.PushCommands }},
		},
		Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
			sid := ctx.Args["session_id"]
			if sid == "" {
				return nil, fmt.Errorf("session_id required")
			}
			return nil, ctx.Client.PushDriver(sid, args["command"], nil)
		},
	})
	if feats.On(features.Peers) {
		r.Register(Tool{
			Name:        "send-to-session",
			Description: "Send message to a session (appears as [peer-msg from=palette])",
			Params: []Param{
				{
					Name: "target",
					Options: func(ctx *ToolContext) []string {
						sessions, _, _, _, err := ctx.Client.ListSessions()
						if err != nil {
							return nil
						}
						opts := make([]string, 0, len(sessions))
						for _, s := range sessions {
							subtitle := s.View.Card.Subtitle
							var label string
							if subtitle != "" {
								label = fmt.Sprintf("%s (%s)", s.Name(), subtitle)
							} else {
								label = s.Name()
							}
							opts = append(opts, s.ID+":"+label)
						}
						return opts
					},
				},
				{
					Name:    "text",
					Options: func(ctx *ToolContext) []string { return nil },
				},
			},
			Run: func(ctx *ToolContext, args map[string]string) (*ToolInvocation, error) {
				target := args["target"]
				text := args["text"]
				if target == "" || text == "" {
					return nil, fmt.Errorf("target and text are required")
				}
				sessionID := target
				if idx := strings.Index(target, ":"); idx > 0 {
					sessionID = target[:idx]
				}
				// Palette has no ROOST_FRAME_ID so we cannot route through
				// peer.send (which requires a frame-to-frame link). Instead,
				// push the message into the session's pane via surface.send_text,
				// formatted so the receiving agent recognises it as a peer message.
				formatted := "[peer-msg from=palette]\n" + text
				bgCtx := context.Background()
				_, err := ctx.Client.Send(bgCtx, proto.CmdSurfaceSendText{
					SessionID: sessionID,
					Text:      formatted,
				})
				return nil, err
			},
		})
	}
	return r
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
