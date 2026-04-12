package driver

import (
	"github.com/takezoh/agent-roost/state"
)

func (d CodexDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	plan, err := CommonPrepareCreate(&cs.CommonState, project, command, "--worktree")
	return cs, plan, err
}

func (d CodexDriver) CompleteCreate(s state.DriverState, command string, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	launch, err := CommonCompleteCreate(&cs.CommonState, command, result, err, "--worktree")
	if err == nil {
		cs.ManagedWorkingDir = cs.WorkingDir
	}
	return cs, launch, err
}

func (d CodexDriver) ManagedWorktreePath(s state.DriverState) string {
	cs, ok := s.(CodexState)
	if !ok {
		return ""
	}
	if path := managedWorktreePath(cs.ManagedWorkingDir); path != "" {
		return path
	}
	return managedWorktreePath(cs.WorkingDir)
}
