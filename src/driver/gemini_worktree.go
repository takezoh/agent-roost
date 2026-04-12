package driver

import (
	"errors"

	"github.com/takezoh/agent-roost/state"
)

func (d GeminiDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	plan, _, err := managedWorktreePlan(project, command, "--worktree", "--workspace")
	if err != nil {
		return gs, state.CreatePlan{}, err
	}
	return gs, plan, nil
}

func (d GeminiDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	if err != nil {
		return gs, state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.WorkingDir == "" {
		return gs, state.CreateLaunch{}, errors.New("worktree setup did not return a working directory")
	}
	gs.WorkingDir = r.WorkingDir
	_, stripped := parseWorktreeFlags(command, "--worktree", "--workspace")
	return gs, state.CreateLaunch{Command: stripped, StartDir: r.WorkingDir}, nil
}

func (d GeminiDriver) ManagedWorktreePath(s state.DriverState) string {
	gs, ok := s.(GeminiState)
	if !ok {
		return ""
	}
	return managedWorktreePath(gs.WorkingDir)
}
