package driver

// Standard tag colors. Drivers reference these via the helper
// constructors below so that color decisions live in one place
// inside the driver package (not in tui/ — Tag colors are
// driver-owned per ARCHITECTURE.md §描画責務の所在).
const (
	commandTagBg = "#78DCE8" // cyan-ish
	commandTagFg = "#1d2021" // dark text on bright bg
	branchTagBg  = "#A9DC76" // green-ish
	branchTagFg  = "#1d2021"
)

// CommandTag returns the canonical command tag for a driver name.
// Every built-in driver puts this as the first entry in
// View().Card.Tags so the user always sees which agent runs in a
// session. Drivers are free to omit it if they have a better label.
func CommandTag(name string) Tag {
	return Tag{
		Text:       name,
		Background: commandTagBg,
		Foreground: commandTagFg,
	}
}

// BranchTag returns the standard git branch tag. Empty branch name
// produces an empty Tag (Text == "") which callers should not append.
func BranchTag(branch string) Tag {
	if branch == "" {
		return Tag{}
	}
	return Tag{
		Text:       branch,
		Background: branchTagBg,
		Foreground: branchTagFg,
	}
}
