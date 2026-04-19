package driver

import "github.com/takezoh/agent-roost/state"

func (d GeminiDriver) PrepareCreate(s state.DriverState, _ state.SessionID, project, command string, options state.LaunchOptions) (state.DriverState, state.CreatePlan, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	plan, err := CommonPrepareCreate(&gs.CommonState, project, command, options, "--worktree", "--workspace")
	return gs, plan, err
}

func (d GeminiDriver) CompleteCreate(s state.DriverState, command string, options state.LaunchOptions, result any, err error) (state.DriverState, state.CreateLaunch, error) {
	gs, ok := s.(GeminiState)
	if !ok {
		gs = GeminiState{}
	}
	launch, err := CommonCompleteCreate(&gs.CommonState, command, options, result, err, "--worktree", "--workspace")
	if err == nil {
		gs.ManagedWorkingDir = gs.StartDir
		launch.Options = state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: true}}
	}
	return gs, launch, err
}

func (d GeminiDriver) ManagedWorktreePath(s state.DriverState) string {
	gs, ok := s.(GeminiState)
	if !ok {
		return ""
	}
	if path := managedWorktreePath(gs.ManagedWorkingDir); path != "" {
		return path
	}
	return managedWorktreePath(gs.StartDir)
}
