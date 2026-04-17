package vt

import (
	"testing"
)

func TestFeedAndSnapshot_Basic(t *testing.T) {
	term := New(40, 10)
	if err := term.Feed([]byte("hello $ ")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap := term.Snapshot()
	if snap.Cols != 40 || snap.Rows != 10 {
		t.Errorf("size = %dx%d, want 40x10", snap.Cols, snap.Rows)
	}
	if snap.LastLine == "" {
		t.Error("LastLine should not be empty after writing text")
	}
	if snap.Stable == "" {
		t.Error("Stable hash should not be empty")
	}
}

func TestFeedAndSnapshot_DirtyCount_SameInput(t *testing.T) {
	term := New(40, 10)
	if err := term.Feed([]byte("static content")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap1 := term.Snapshot()
	// Feed the same data again (no visual change).
	if err := term.Feed([]byte("")); err != nil {
		t.Fatalf("Feed empty: %v", err)
	}
	snap2 := term.Snapshot()
	if snap1.Stable != snap2.Stable {
		t.Errorf("Stable changed without screen change: %q → %q", snap1.Stable, snap2.Stable)
	}
	if snap2.DirtyCount != 0 {
		t.Errorf("DirtyCount = %d, want 0 when screen unchanged", snap2.DirtyCount)
	}
}

func TestFeedAndSnapshot_DirtyCount_Changed(t *testing.T) {
	term := New(40, 10)
	if err := term.Feed([]byte("first")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap1 := term.Snapshot()
	if err := term.Feed([]byte(" second")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap2 := term.Snapshot()
	if snap1.Stable == snap2.Stable {
		t.Error("Stable should differ after writing new content")
	}
	if snap2.DirtyCount == 0 {
		t.Error("DirtyCount should be >0 when screen changed")
	}
}

func TestFeedAndSnapshot_OscHyperlink(t *testing.T) {
	term := New(40, 10)
	// OSC 8 hyperlink: ESC]8;;https://example.com\aLinkText\ESC]8;;\a
	link := "\x1b]8;;https://example.com\x07Link\x1b]8;;\x07"
	if err := term.Feed([]byte(link)); err != nil {
		t.Fatalf("Feed OSC 8: %v", err)
	}
	snap := term.Snapshot()
	// Verify no OSC 8 ends up in Notifications (OSC 8 is not 9/99/777).
	for _, n := range snap.Notifications {
		if n.Cmd == 8 {
			t.Errorf("OSC 8 should not appear in Notifications, got %+v", n)
		}
	}
	// The hyperlink URL should be stored in the cell's Link.URL — we don't
	// expose CellAt directly from Terminal, but we verify no panic / error.
}

func TestFeedAndSnapshot_OscNotification9(t *testing.T) {
	term := New(40, 10)
	// iTerm2 OSC 9: ESC]9;Hello from agent\a
	osc9 := "\x1b]9;Hello from agent\x07"
	if err := term.Feed([]byte(osc9)); err != nil {
		t.Fatalf("Feed OSC 9: %v", err)
	}
	snap := term.Snapshot()
	if len(snap.Notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(snap.Notifications))
	}
	n := snap.Notifications[0]
	if n.Cmd != 9 {
		t.Errorf("Cmd = %d, want 9", n.Cmd)
	}
	if n.Payload != "Hello from agent" {
		t.Errorf("Payload = %q, want %q", n.Payload, "Hello from agent")
	}
}

func TestFeedAndSnapshot_OscNotification777(t *testing.T) {
	term := New(40, 10)
	// urxvt OSC 777: ESC]777;notify;Title;Body\a
	osc777 := "\x1b]777;notify;MyTitle;MyBody\x07"
	if err := term.Feed([]byte(osc777)); err != nil {
		t.Fatalf("Feed OSC 777: %v", err)
	}
	snap := term.Snapshot()
	if len(snap.Notifications) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(snap.Notifications))
	}
	n := snap.Notifications[0]
	if n.Cmd != 777 {
		t.Errorf("Cmd = %d, want 777", n.Cmd)
	}
	if n.Payload != "notify;MyTitle;MyBody" {
		t.Errorf("Payload = %q, want %q", n.Payload, "notify;MyTitle;MyBody")
	}
}

func TestFeedAndSnapshot_NotificationsFlushOnSnapshot(t *testing.T) {
	term := New(40, 10)
	osc9 := "\x1b]9;once\x07"
	if err := term.Feed([]byte(osc9)); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap1 := term.Snapshot()
	if len(snap1.Notifications) != 1 {
		t.Fatalf("first snapshot: expected 1 notification, got %d", len(snap1.Notifications))
	}
	snap2 := term.Snapshot()
	if len(snap2.Notifications) != 0 {
		t.Errorf("second snapshot: expected 0 notifications, got %d (should flush)", len(snap2.Notifications))
	}
}

func TestResize(t *testing.T) {
	term := New(40, 10)
	term.Resize(100, 30)
	snap := term.Snapshot()
	if snap.Cols != 100 || snap.Rows != 30 {
		t.Errorf("after resize: size = %dx%d, want 100x30", snap.Cols, snap.Rows)
	}
}

func TestReset(t *testing.T) {
	term := New(40, 10)
	if err := term.Feed([]byte("some content")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	snap1 := term.Snapshot()
	term.Reset()
	snap2 := term.Snapshot()
	if snap1.LastLine == snap2.LastLine && snap1.LastLine != "" {
		t.Error("Reset should clear screen; LastLine should be empty")
	}
	if snap2.DirtyCount != 0 {
		t.Error("DirtyCount after Reset should be 0 (no previous baseline)")
	}
}
