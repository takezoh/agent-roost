package vcs

import (
	"github.com/take/agent-roost/lib/git"
	"github.com/take/agent-roost/lib/plastic"
)

// Result holds the detected branch name and brand colors for display.
type Result struct {
	Branch     string // branch name (empty if no VCS detected)
	Background string // brand color hex (e.g. "#F05032")
	Foreground string // text color hex (e.g. "#FFFFFF")
}

// Brand colors per VCS.
const (
	gitBG     = "#F05032" // Git brand orange-red
	plasticBG = "#00ADEF" // Plastic SCM brand blue
	defaultFG = "#FFFFFF" // white text on brand backgrounds
)

// DetectBranch tries each supported VCS in order and returns the first
// successful result. Order: git → Plastic SCM.
func DetectBranch(dir string) Result {
	if b := git.DetectBranch(dir); b != "" {
		return Result{Branch: b, Background: gitBG, Foreground: defaultFG}
	}
	if b := plastic.DetectBranch(dir); b != "" {
		return Result{Branch: b, Background: plasticBG, Foreground: defaultFG}
	}
	return Result{}
}
