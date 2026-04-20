package driver

import (
	"fmt"
	"path/filepath"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"

	"github.com/takezoh/agent-roost/state"
)

type worktreeRequest struct {
	Enabled bool
	Name    string
}

const worktreeNameAttempts = 5

func parseWorktreeFlags(command string, flags ...string) (worktreeRequest, string) {
	parts := strings.Fields(command)
	out := make([]string, 0, len(parts))
	var req worktreeRequest
	for i := 0; i < len(parts); i++ {
		p := parts[i]
		matched := false
		for _, flag := range flags {
			switch {
			case p == flag:
				req.Enabled = true
				matched = true
				if i+1 < len(parts) && !strings.HasPrefix(parts[i+1], "-") {
					req.Name = parts[i+1]
					i++
				}
			case strings.HasPrefix(p, flag+"="):
				req.Enabled = true
				req.Name = strings.TrimPrefix(p, flag+"=")
				matched = true
			}
			if matched {
				break
			}
		}
		if !matched {
			out = append(out, p)
		}
	}
	return req, strings.Join(out, " ")
}

func resolveWorktreeRequest(command string, options state.LaunchOptions, flags ...string) (worktreeRequest, string) {
	req, stripped := parseWorktreeFlags(command, flags...)
	if options.Worktree.Enabled {
		req.Enabled = true
	}
	return req, strings.TrimSpace(stripped)
}

func appendFlag(command, flag string, enabled bool) string {
	command = strings.TrimSpace(command)
	if !enabled || command == "" {
		return command
	}
	return strings.TrimSpace(command + " " + flag)
}

func generatedWorktreeNames() []string {
	out := make([]string, 0, worktreeNameAttempts)
	seen := map[string]struct{}{}
	for len(out) < worktreeNameAttempts {
		name := petname.Generate(4, "-")
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func managedWorktreePlan(project, command string, options state.LaunchOptions, flags ...string) (state.CreatePlan, string, error) {
	req, stripped := resolveWorktreeRequest(command, options, flags...)
	if !req.Enabled {
		return state.CreatePlan{
			Launch: state.CreateLaunch{
				Command:  strings.TrimSpace(command),
				StartDir: project,
				Options:  state.LaunchOptions{},
			},
		}, "", nil
	}
	names := []string{req.Name}
	if req.Name == "" {
		names = generatedWorktreeNames()
		if len(names) == 0 {
			return state.CreatePlan{}, "", fmt.Errorf("failed to generate worktree names")
		}
	}
	return state.CreatePlan{
		Launch: state.CreateLaunch{
			Command: strings.TrimSpace(stripped),
			Options: state.LaunchOptions{Worktree: state.WorktreeOption{Enabled: true}},
		},
		SetupJob: WorktreeSetupInput{
			RepoDir:        project,
			CandidateNames: names,
		},
	}, names[0], nil
}

func managedWorktreePath(path string) string {
	if path == "" || !isManagedWorktreePath(path) {
		return ""
	}
	return path
}

func isManagedWorktreePath(path string) bool {
	clean := filepath.Clean(path)
	parent := filepath.Base(filepath.Dir(clean))
	grand := filepath.Base(filepath.Dir(filepath.Dir(clean)))
	return parent == "worktrees" && grand == ".roost"
}

// CommonPrepareCreate handles the shared logic for preparing a worktree-enabled session.
func CommonPrepareCreate(c *CommonState, project, command string, options state.LaunchOptions, flags ...string) (state.CreatePlan, error) {
	plan, name, err := managedWorktreePlan(project, command, options, flags...)
	if err != nil {
		return state.CreatePlan{}, err
	}
	if name != "" {
		c.WorktreeName = name
	}
	return plan, nil
}

// CommonCompleteCreate handles the shared logic for completing a worktree-enabled session creation.
func CommonCompleteCreate(c *CommonState, command string, options state.LaunchOptions, result any, err error, flags ...string) (state.CreateLaunch, error) {
	if err != nil {
		return state.CreateLaunch{}, err
	}
	r, ok := result.(WorktreeSetupResult)
	if !ok || r.StartDir == "" {
		return state.CreateLaunch{}, fmt.Errorf("worktree setup did not return a working directory")
	}
	c.StartDir = r.StartDir
	if r.Name != "" {
		c.WorktreeName = r.Name
	}
	return state.CreateLaunch{
		Command:  strings.TrimSpace(command),
		StartDir: r.StartDir,
		Options:  options,
	}, nil
}
