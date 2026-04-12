package driver

import (
	"errors"

	"github.com/takezoh/agent-roost/state"
)

func (d ClaudeDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	plan, name, err := managedWorktreePlan(project, command, "--worktree")
	if err != nil {
		return cs, state.CreatePlan{}, err
	}
	_ = name
	return cs, plan, nil
}

func (d ClaudeDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	if err != nil {
		return cs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.WorkingDir == "" {
		return cs, state.CreateLaunch{}, errors.New("worktree setup did not return a working directory")
	}
	cs.WorkingDir = r.WorkingDir
	_, stripped := parseWorktreeFlags(command, "--worktree")
	return cs, state.CreateLaunch{Command: stripped, StartDir: r.WorkingDir}, nil
}

func (d ClaudeDriver) ManagedWorktreePath(s state.DriverState) string {
	cs, ok := s.(ClaudeState)
	if !ok {
		return ""
	}
	return managedWorktreePath(cs.WorkingDir)
}
