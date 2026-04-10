package tui

// Pane IDs for the tmux window the TUI runs inside. The layout is fixed
// by main.go: main agent pane on top, log pane below it, sidebar on the
// right. focus-pane and pane-focused events identify panes by these IDs.
const (
	mainPane    = "0.0"
	logPane     = "0.1"
	sidebarPane = "0.2"
)
