package driver

import "github.com/take/agent-roost/state"

// Standard tag colors. Drivers reference these via the helper
// constructors below so that color decisions live in one place inside
// the driver package (not in tui/) — Tag colors are driver-owned per
// ARCHITECTURE.md §描画責務の所在.
const (
	commandTagBg = "#D97757" // Claude brand orange
	commandTagFg = "#FFFFFF" // white text
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

// BranchTag returns a VCS branch tag with pre-resolved brand colors.
// Empty branch name produces an empty Tag (Text == "") which callers
// should not append.
func BranchTag(branch, bg, fg string) state.Tag {
	if branch == "" {
		return state.Tag{}
	}
	return state.Tag{
		Text:       branch,
		Background: bg,
		Foreground: fg,
	}
}
