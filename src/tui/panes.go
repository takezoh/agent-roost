package tui

// Pane IDs for the tmux window the TUI runs inside. The layout is fixed
// by main.go: header at the top (0.0), main pane below it (0.1), log
// pane below main (0.2), and the session sidebar on the right (0.3).
// focus-pane and pane-focused events identify panes by these IDs.
const (
	headerPane  = "0.0"
	mainPane    = "0.1"
	logPane     = "0.2"
	sidebarPane = "0.3"
)
