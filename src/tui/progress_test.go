package tui

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/proto"
	"github.com/takezoh/agent-roost/state"
)

func sessionAtState(st state.Status, since time.Duration) *proto.SessionInfo {
	t := time.Now().Add(-since).Format(time.RFC3339)
	return &proto.SessionInfo{
		State:          st,
		StateChangedAt: t,
	}
}

func TestRunningFractionZeroWhenNotRunning(t *testing.T) {
	s := sessionAtState(state.StatusIdle, 30*time.Second)
	if f := runningFraction(s); f != 0 {
		t.Fatalf("want 0 for non-Running, got %f", f)
	}
}

func TestRunningFractionNilSession(t *testing.T) {
	if f := runningFraction(nil); f != 0 {
		t.Fatalf("want 0 for nil, got %f", f)
	}
}

func TestRunningFractionQuarter(t *testing.T) {
	s := sessionAtState(state.StatusRunning, 30*time.Second) // 30/120 = 0.25
	f := runningFraction(s)
	if f < 0.2 || f > 0.3 {
		t.Fatalf("want ~0.25, got %f", f)
	}
}

func TestRunningFractionClampsToOne(t *testing.T) {
	s := sessionAtState(state.StatusRunning, 200*time.Second) // > 120s
	if f := runningFraction(s); f != 1 {
		t.Fatalf("want 1 when elapsed > limit, got %f", f)
	}
}

func TestRenderRunningProgressEmptyWhenNotRunning(t *testing.T) {
	s := sessionAtState(state.StatusStopped, 30*time.Second)
	if got := renderRunningProgress(s, 40); got != "" {
		t.Fatalf("expected empty string for Stopped, got %q", got)
	}
}

func TestRenderRunningProgressNonEmptyWhenRunning(t *testing.T) {
	s := sessionAtState(state.StatusRunning, 30*time.Second)
	got := renderRunningProgress(s, 40)
	if got == "" {
		t.Fatal("expected non-empty progress bar for Running session")
	}
}

func TestRenderIndeterminateProgressNonEmpty(t *testing.T) {
	got := renderIndeterminateProgress(40)
	if got == "" {
		t.Fatal("indeterminate progress bar should be non-empty for width 40")
	}
}

func TestRenderIndeterminateProgressEmptyWhenTooNarrow(t *testing.T) {
	if got := renderIndeterminateProgress(3); got != "" {
		t.Fatalf("expected empty for width < 4, got %q", got)
	}
}

func TestRenderIndeterminateProgressAdvancesWithFrame(t *testing.T) {
	animFrame = 0
	a := renderIndeterminateProgress(40)
	animFrame = 30 // advance enough to move the highlight
	b := renderIndeterminateProgress(40)
	animFrame = 0
	if a == b {
		t.Error("shimmer should differ across frames")
	}
}

func TestRunningProgressUsesShimmerWhenStateChangedAtUnknown(t *testing.T) {
	s := &proto.SessionInfo{State: state.StatusRunning, StateChangedAt: ""}
	got := renderRunningProgress(s, 40)
	if got == "" {
		t.Fatal("Running session with no StateChangedAt should show shimmer, not empty")
	}
}
