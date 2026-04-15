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

// rebuildSpinnerPalette computes a 24-stop green gradient anchored on the
// theme's Running color. Called from ApplyTheme.
func rebuildSpinnerPalette(t Theme) {
	runningPalette = lipgloss.Blend1D(24,
		lipgloss.Color("#003c00"),
		lipgloss.Color("#00a000"),
		t.Running,
		lipgloss.Color("#7fffa0"),
		lipgloss.Color("#00ff80"),
		lipgloss.Color("#00a040"),
	)
}

// runningSpinnerGlyph returns the current braille glyph styled with the
// current palette color, based on the global animFrame counter.
func runningSpinnerGlyph() string {
	frames := spinner.MiniDot.Frames
	g := frames[int(animFrame)%len(frames)]
	c := runningPalette[int(animFrame)%len(runningPalette)]
	return lipgloss.NewStyle().Foreground(c).Render(g)
}
