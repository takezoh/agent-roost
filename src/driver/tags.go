package driver

import "github.com/takezoh/agent-roost/state"

// Standard tag colors. Drivers reference these via the helper
// constructors below so that color decisions live in one place inside
// the driver package (not in tui/) — Tag colors are driver-owned per
// ARCHITECTURE.md §描画責務の所在.
const (
	commandTagBg = "#D97757" // default command tag background
	commandTagFg = "#FFFFFF" // white text
	codexTagBg   = "#10A37F"
	codexTagFg   = "#FFFFFF"
)

// CommandTag returns the canonical command tag for a driver name, used
// as BorderTitle in session cards.
func CommandTag(name string) state.Tag {
	return state.Tag{
		Text:       name,
		Background: commandTagBg,
		Foreground: commandTagFg,
	}
}

func CodexCommandTag() state.Tag {
	return state.Tag{
		Text:       CodexDriverName,
		Background: codexTagBg,
		Foreground: codexTagFg,
	}
}

// BranchTag returns a VCS branch tag with pre-resolved brand colors.
// When parentBranch is non-empty the tag text includes an arrow
// showing the main worktree's branch (e.g. "feature ← main").
// Empty branch name produces an empty Tag (Text == "") which callers
// should not append.
func BranchTag(branch, bg, fg, parentBranch string) state.Tag {
	if branch == "" {
		return state.Tag{}
	}
	text := branch
	if parentBranch != "" {
		text = branch + " \u2190 " + parentBranch
	}
	return state.Tag{
		Text:       text,
		Background: bg,
		Foreground: fg,
	}
}
