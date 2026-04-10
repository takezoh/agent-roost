package vcs

import (
	"github.com/take/agent-roost/lib/git"
	"github.com/take/agent-roost/lib/plastic"
)

// Result holds the detected branch name and which VCS it came from.
type Result struct {
	Branch string // branch name (empty if no VCS detected)
	VCS    string // "git", "plastic", or ""
}

// DetectBranch tries each supported VCS in order and returns the first
// successful result. Order: git → Plastic SCM.
func DetectBranch(dir string) Result {
	if b := git.DetectBranch(dir); b != "" {
		return Result{Branch: b, VCS: "git"}
	}
	if b := plastic.DetectBranch(dir); b != "" {
		return Result{Branch: b, VCS: "plastic"}
	}
	return Result{}
}
