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

// hostColors maps git hosting provider hostnames to their brand colors.
var hostColors = map[string]string{
	"github.com":    "#24292F", // GitHub dark
	"gitlab.com":    "#FC6D26", // GitLab orange
	"bitbucket.org": "#0052CC", // Bitbucket blue
	"codeberg.org":  "#2185D0", // Codeberg blue
	"sr.ht":         "#888888", // SourceHut grey
}

func resolveGitBackground(dir string) string {
	if bg, ok := hostColors[git.DetectRemoteHost(dir)]; ok {
		return bg
	}
	return gitBG
}

// DetectBranch tries each supported VCS in order and returns the first
// successful result. Order: git → Plastic SCM.
func DetectBranch(dir string) Result {
	if b := git.DetectBranch(dir); b != "" {
		return Result{Branch: b, Background: resolveGitBackground(dir), Foreground: defaultFG}
	}
	if b := plastic.DetectBranch(dir); b != "" {
		return Result{Branch: b, Background: plasticBG, Foreground: defaultFG}
	}
	return Result{}
}
