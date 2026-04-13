package driver

import (
	"github.com/takezoh/agent-roost/state"
)

func (d ClaudeDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string, options state.LaunchOptions) (state.DriverState, state.CreatePlan, error) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	plan, err := CommonPrepareCreate(&cs.CommonState, project, command, options, "--worktree")
	return cs, plan, err
}

func (d ClaudeDriver) CompleteCreate(s state.DriverState, command string, options state.LaunchOptions, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	cs, ok := s.(ClaudeState)
	if !ok {
		cs = ClaudeState{}
	}
	launch, err := CommonCompleteCreate(&cs.CommonState, command, options, result, err, "--worktree")
	return cs, launch, err
}

func (d ClaudeDriver) ManagedWorktreePath(s state.DriverState) string {
	cs, ok := s.(ClaudeState)
	if !ok {
		return ""
	}
	return managedWorktreePath(cs.WorkingDir)
}
