package driver

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dustinkirkland/golang-petname"
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

func managedWorktreePlan(project, command string, flags ...string) (state.CreatePlan, string, error) {
	req, stripped := parseWorktreeFlags(command, flags...)
	if !req.Enabled {
		return state.CreatePlan{
			Launch: state.CreateLaunch{Command: command, StartDir: project},
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
		Launch: state.CreateLaunch{Command: stripped},
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
