package tui

import (
	"image/color"

	"charm.land/bubbles/v2/spinner"
	"charm.land/lipgloss/v2"
)

// animFrame is incremented on every spinner.TickMsg. Views read this value
// directly — no locking needed because Bubble Tea guarantees single-threaded
// model updates.
var animFrame uint64

// runningPalette is a pre-computed gradient cycled through while a session is
// in the running state. Rebuilt by rebuildSpinnerPalette whenever the active
// theme changes.
var runningPalette []color.Color

// rebuildSpinnerPalette computes a 24-stop gradient from the theme's
// RunningGradientA→B anchor colors. Called from ApplyTheme.
func rebuildSpinnerPalette(t Theme) {
	runningPalette = lipgloss.Blend1D(24, t.RunningGradientA, t.RunningGradientB)
}

// runningSpinnerGlyph returns the current spinner glyph styled with the
// gradient palette color, based on the global animFrame counter.
func runningSpinnerGlyph() string {
	frames := spinner.MiniDot.Frames
	g := frames[int(animFrame)%len(frames)]
	c := runningPalette[int(animFrame)%len(runningPalette)]
	return lipgloss.NewStyle().Foreground(c).Render(g)
}

// waitingSpinnerGlyph returns a pulsing block glyph in the Waiting color to
// visually distinguish the waiting-for-input state from running.
func waitingSpinnerGlyph() string {
	frames := spinner.Pulse.Frames // ["█","▓","▒","░"]
	g := frames[int(animFrame)%len(frames)]
	return waitingStyle.Render(g)
}
