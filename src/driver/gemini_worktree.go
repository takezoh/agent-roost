package driver

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

const (
	geminiKeyManagedWorkingDir = "managed_working_dir"
	geminiKeyWorktreeName      = "worktree_name"
)

type geminiWorktreeRequest struct {
	Enabled bool
	Name    string
}

func parseGeminiWorktree(command string) (geminiWorktreeRequest, string) {
	parts := strings.Fields(command)
	out := make([]string, 0, len(parts))
	var req geminiWorktreeRequest
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		switch {
		case p == "--worktree" || p == "--workspace":
			req.Enabled = true
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				req.Name = parts[i+1]
				i++
			}
		case strings.HasPrefix(p, "--worktree=") || strings.HasPrefix(p, "--workspace="):
			req.Enabled = true
			if strings.HasPrefix(p, "--worktree=") {
				req.Name = strings.TrimPrefix(p, "--worktree=")
			} else {
				req.Name = strings.TrimPrefix(p, "--workspace=")
			}
		default:
			out = append(out, p)
		}
	}
	return req, strings.Join(out, " ")
}

func generatedGeminiWorktreeName(sessionID state.SessionID) string {
	id := string(sessionID)
	if len(id) > 8 {
		id = id[:8]
	}
	return "gemini-" + id
}

func (d GeminiDriver) PrepareCreate(s state.DriverState, sessionID state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	req, stripped := parseGeminiWorktree(command)
	slog.Debug("gemini prepare create", "command", command, "req", req, "stripped", stripped)
	if !req.Enabled {
		return gs, state.CreatePlan{
			Launch: state.CreateLaunch{Command: command, StartDir: project},
		}, nil
	}
	name := req.Name
	if name == "" {
		name = generatedGeminiWorktreeName(sessionID)
	}
	gs.WorktreeName = name
	slog.Debug("gemini prepare create worktree", "name", name, "project", project)
	return gs, state.CreatePlan{
		Launch:   state.CreateLaunch{Command: stripped},
		SetupJob: WorktreeSetupInput{RepoDir: project, Name: name},
	}, nil
}

func (d GeminiDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	if err != nil {
		slog.Error("gemini complete create error", "err", err)
		return gs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.WorkingDir == "" {
		slog.Error("gemini complete create unexpected result", "result", result)
		return gs, state.CreateLaunch{}, fmt.Errorf("worktree setup did not return a working directory")
	}
	slog.Debug("gemini complete create", "dir", r.WorkingDir, "name", r.Name)
	gs.ManagedWorkingDir = r.WorkingDir
	gs.WorkingDir = r.WorkingDir
	if r.Name != "" {
		gs.WorktreeName = r.Name
	}
	_, stripped := parseGeminiWorktree(command)
	return gs, state.CreateLaunch{
		Command:  stripped,
		StartDir: r.WorkingDir,
	}, nil
}
