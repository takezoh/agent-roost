package driver

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver/vt"
	"github.com/takezoh/agent-roost/state"
)

// baseCommonState returns a CommonState primed for hang-detection tests.
func baseCommonState(now time.Time) CommonState {
	var c CommonState
	c.Status = state.StatusRunning
	c.StatusChangedAt = now
	c.PaneHash = "stable-hash"
	c.PaneHashAt = now
	c.PaneLastLine = "stable line"
	c.PaneLastLineAt = now
	return c
}

func TestHangNotTriggeredWhenLastLineChanges(t *testing.T) {
	now := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	c := baseCommonState(now)

	// LastLine changes just before threshold
	recentLine := now.Add(commonHangThreshold - 10*time.Second)
	c.PaneLastLineAt = recentLine

	tick := state.DEvTick{
		Now:    now.Add(commonHangThreshold + time.Second),
		Active: false,
	}
	effs := c.HandleTick(tick, false)

	if c.Status == state.StatusStopped {
		t.Error("should not hang when LastLine changed recently")
	}
	for _, eff := range effs {
		if _, ok := eff.(state.EffEventLogAppend); ok {
			t.Error("should not emit HangDetected when LastLine changed recently")
		}
	}
}

func TestHangTriggeredWhenBothHashAndLastLineStale(t *testing.T) {
	now := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)
	c := baseCommonState(now)

	tick := state.DEvTick{
		Now:    now.Add(commonHangThreshold + 10*time.Second),
		Active: false,
	}
	effs := c.HandleTick(tick, false)

	if c.Status != state.StatusStopped {
		t.Error("should hang when both hash and last line are stale")
	}
	if !c.HangDetected {
		t.Error("HangDetected should be true")
	}
	found := false
	for _, eff := range effs {
		if _, ok := eff.(state.EffEventLogAppend); ok {
			found = true
		}
	}
	if !found {
		t.Error("expected EffEventLogAppend for hang")
	}
}

func TestHandleCapturePaneResultUpdatesLastLine(t *testing.T) {
	var c CommonState
	now := time.Date(2026, 4, 17, 0, 0, 0, 0, time.UTC)

	// First capture primes LastLine
	c.HandleCapturePaneResult(CapturePaneResult{
		Snapshot: vt.Snapshot{Stable: "hash1", LastLine: "first line"},
	}, nil, now)
	if c.PaneLastLine != "first line" {
		t.Errorf("PaneLastLine = %q, want first line", c.PaneLastLine)
	}
	if !c.PaneLastLineAt.Equal(now) {
		t.Error("PaneLastLineAt should be set to now")
	}

	// Second capture with same LastLine: timestamp should not update
	later := now.Add(5 * time.Second)
	c.HandleCapturePaneResult(CapturePaneResult{
		Snapshot: vt.Snapshot{Stable: "hash2", LastLine: "first line"},
	}, nil, later)
	if !c.PaneLastLineAt.Equal(now) {
		t.Error("PaneLastLineAt should not update when LastLine unchanged")
	}

	// Third capture with changed LastLine: timestamp should update
	c.HandleCapturePaneResult(CapturePaneResult{
		Snapshot: vt.Snapshot{Stable: "hash3", LastLine: "new line"},
	}, nil, later)
	if c.PaneLastLine != "new line" {
		t.Errorf("PaneLastLine = %q, want new line", c.PaneLastLine)
	}
	if !c.PaneLastLineAt.Equal(later) {
		t.Error("PaneLastLineAt should update when LastLine changes")
	}
}
