package driver

import (
	"github.com/takezoh/agent-roost/state"
)

func (d GeminiDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	plan, err := CommonPrepareCreate(&gs.CommonState, project, command, "--worktree", "--workspace")
	return gs, plan, err
}

func (d GeminiDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	launch, err := CommonCompleteCreate(&gs.CommonState, command, result, err, "--worktree", "--workspace")
	return gs, launch, err
}

func (d GeminiDriver) ManagedWorktreePath(s state.DriverState) string {
	gs, ok := s.(GeminiState)
	if !ok {
		return ""
	}
	return managedWorktreePath(gs.WorkingDir)
}
