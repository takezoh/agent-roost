package driver

import (
	"path/filepath"
	"strings"

	"github.com/takezoh/agent-roost/state"
)

// Standard tag colors. Drivers reference these via the helper
// constructors below so that color decisions live in one place inside
// the driver package (not in tui/) — Tag colors are driver-owned per
// ARCHITECTURE.md §Rendering Ownership.
const (
	commandTagBg = "#D97757" // default command tag background
	commandTagFg = "#FFFFFF" // white text
	codexTagBg   = "#10A37F"
	codexTagFg   = "#FFFFFF"
	geminiTagBg  = "#1A73E8"
	geminiTagFg  = "#FFFFFF"

	// shell brand colors
	bashTagBg       = "#4EAA25" // GNU bash logo green
	zshTagBg        = "#2D6DB5" // Z Shell blue
	fishTagBg       = "#F57900" // fish-shell orange
	powershellTagBg = "#012456" // classic PowerShell navy
	powershellTagFg = "#EEEDF0"
	nushellTagBg    = "#3AA675" // Nushell logo green
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

// ShellCommandTag returns a command tag colored with the brand color of
// well-known shells. name is matched case-insensitively against the
// basename so that full paths (e.g. /usr/bin/bash) work correctly.
// Unknown shell names fall back to the default command tag color.
func ShellCommandTag(name string) state.Tag {
	key := strings.ToLower(filepath.Base(name))
	bg, fg := commandTagBg, commandTagFg
	switch key {
	case "bash":
		bg = bashTagBg
	case "zsh":
		bg = zshTagBg
	case "fish":
		bg = fishTagBg
	case "powershell", "pwsh":
		bg, fg = powershellTagBg, powershellTagFg
	case "nu", "nushell":
		bg = nushellTagBg
	}
	return state.Tag{Text: name, Background: bg, Foreground: fg}
}

func CodexCommandTag() state.Tag {
	return state.Tag{
		Text:       CodexDriverName,
		Background: codexTagBg,
		Foreground: codexTagFg,
	}
}

func GeminiCommandTag() state.Tag {
	return state.Tag{
		Text:       GeminiDriverName,
		Background: geminiTagBg,
		Foreground: geminiTagFg,
	}
}

// BranchTag returns a VCS branch tag with pre-resolved brand colors.
// When parentBranch is non-empty the tag text includes an arrow
// showing the main worktree's branch (e.g. "feature → main").
// Empty branch name produces an empty Tag (Text == "") which callers
// should not append.
func BranchTag(branch, bg, fg, parentBranch string) state.Tag {
	if branch == "" {
		return state.Tag{}
	}
	text := branch
	if parentBranch != "" {
		text = branch + " \u2192 " + parentBranch
	}
	return state.Tag{
		Text:       text,
		Background: bg,
		Foreground: fg,
	}
}
