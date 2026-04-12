package driver

import (
	"fmt"

	"github.com/takezoh/agent-roost/state"
)

const (
	codexKeyManagedWorkingDir = "managed_working_dir"
	codexKeyWorktreeName      = "worktree_name"
)

func (d CodexDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string) (state.DriverState, state.CreatePlan, error) {
	cs, ok := s.(CodexState)
	if !ok {
		cs = CodexState{}
	}
	plan, name, err := managedWorktreePlan(project, command, "--worktree")
	if err != nil {
		return cs, state.CreatePlan{}, err
	}
	if name != "" {
		cs.WorktreeName = name
	}
	return cs, plan, nil
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
	_, stripped := parseWorktreeFlags(command, "--worktree")
	return cs, state.CreateLaunch{
		Command:  stripped,
		StartDir: r.WorkingDir,
	}, nil
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
