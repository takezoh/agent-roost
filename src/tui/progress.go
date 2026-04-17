package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"
	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

// hangLimit is the duration used to scale the hang-detection progress bar.
// It intentionally matches driver.commonHangThreshold (120 s). If that
// constant changes, this value must be updated to stay in sync.
// A cleaner long-term solution is to propagate the threshold via
// proto.SessionInfo so the TUI never hard-codes driver internals.
const hangLimit = 120 * time.Second

// runningFraction returns how full the hang-detection bar should be for the
// given session. Returns 0 when the session is not Running.
func runningFraction(s *proto.SessionInfo) float64 {
	if s == nil || s.State != state.StatusRunning {
		return 0
	}
	changed := s.StateChangedAtTime()
	if changed.IsZero() {
		return 0
	}
	elapsed := time.Since(changed)
	f := float64(elapsed) / float64(hangLimit)
	if f > 1 {
		f = 1
	}
	if f < 0 {
		f = 0
	}
	return f
}

// renderRunningProgress returns a single-line progress bar for a Running
// session. When StateChangedAt is known, shows elapsed vs hang threshold as
// a gradient fill. When unknown, shows an indeterminate shimmer animation.
// Returns "" when the session is not Running.
func renderRunningProgress(s *proto.SessionInfo, width int) string {
	if s == nil || s.State != state.StatusRunning {
		return ""
	}
	if width <= 0 {
		width = 40
	}
	f := runningFraction(s)
	if f == 0 {
		return renderIndeterminateProgress(width)
	}
	bar := progress.New(
		progress.WithColors(runningPalette...),
		progress.WithoutPercentage(),
		progress.WithWidth(width),
	)
	return bar.ViewAs(f)
}

// renderIndeterminateProgress renders a shimmer bar: a 1/4-width highlight
// block that slides left→right→left using the global animFrame counter.
func renderIndeterminateProgress(width int) string {
	if width < 4 {
		return ""
	}
	blockW := width / 4
	if blockW < 2 {
		blockW = 2
	}
	travelW := width - blockW
	// slow down: advance 1 position every 3 frames
	step := int(animFrame) / 3
	pos := step % (travelW * 2)
	if pos > travelW {
		pos = travelW*2 - pos
	}
	hlColor := runningPalette[int(animFrame)%len(runningPalette)]
	bgStyle := lipgloss.NewStyle().Background(lipgloss.Color("#282828"))
	hlStyle := lipgloss.NewStyle().Background(hlColor)
	before := bgStyle.Render(strings.Repeat(" ", pos))
	hl := hlStyle.Render(strings.Repeat(" ", blockW))
	after := bgStyle.Render(strings.Repeat(" ", width-pos-blockW))
	return before + hl + after
}
