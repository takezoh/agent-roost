package driver

import (
	"fmt"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

const (
	codexKeyManagedWorkingDir = "managed_working_dir"
	codexKeyWorktreeName      = "worktree_name"
)

type codexWorktreeRequest struct {
	Enabled bool
	Name    string
}

func parseCodexWorktree(command string) (codexWorktreeRequest, string) {
	parts := strings.Fields(command)
	out := make([]string, 0, len(parts))
	var req codexWorktreeRequest
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		switch {
		case p == "--worktree":
			req.Enabled = true
			if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
				req.Name = parts[i+1]
				i++
			}
		case strings.HasPrefix(p, "--worktree="):
			req.Enabled = true
			req.Name = strings.TrimPrefix(p, "--worktree=")
		default:
			out = append(out, p)
		}
	}
	return req, strings.Join(out, " ")
}

func generatedWorktreeName(sessionID state.SessionID) string {
	id := string(sessionID)
	if len(id) > 8 {
		id = id[:8]
	}
	return "codex-" + id
}

func (d CodexDriver) PrepareCreate(s state.DriverState, sessionID state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	req, stripped := parseCodexWorktree(command)
	if !req.Enabled {
		return cs, state.CreatePlan{
			Launch: state.CreateLaunch{Command: command, StartDir: project},
		}, nil
	}
	name := req.Name
	if name == "" {
		name = generatedWorktreeName(sessionID)
	}
	cs.WorktreeName = name
	return cs, state.CreatePlan{
		Launch:   state.CreateLaunch{Command: stripped},
		SetupJob: WorktreeSetupInput{RepoDir: project, Name: name},
	}, nil
}

func (d CodexDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	if err != nil {
		return cs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.WorkingDir == "" {
		return cs, state.CreateLaunch{}, fmt.Errorf("worktree setup did not return a working directory")
	}
	cs.ManagedWorkingDir = r.WorkingDir
	cs.WorkingDir = r.WorkingDir
	if r.Name != "" {
		cs.WorktreeName = r.Name
	}
	_, stripped := parseCodexWorktree(command)
	return cs, state.CreateLaunch{
		Command:  stripped,
		StartDir: r.WorkingDir,
	}, nil
}
