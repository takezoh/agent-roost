package tui

import (
	"time"

	"charm.land/bubbles/v2/progress"
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
// session, showing elapsed time relative to the hang threshold. Returns ""
// when the session is not Running or StateChangedAt is unknown.
func renderRunningProgress(s *proto.SessionInfo, width int) string {
	if s == nil || s.State != state.StatusRunning {
		return ""
	}
	f := runningFraction(s)
	if f == 0 {
		return "" // StateChangedAt unknown or elapsed rounds to zero
	}
	if width <= 0 {
		width = 40
	}
	bar := progress.New(
		progress.WithColors(runningPalette...),
		progress.WithoutPercentage(),
		progress.WithWidth(width),
	)
	return bar.ViewAs(f)
}
