package state

import (
	"testing"
	"time"
)

func TestNewReturnsInitializedMaps(t *testing.T) {
	s := New()
	if s.Sessions == nil {
		t.Error("Sessions map should be initialized")
	}
	if s.Subscribers == nil {
		t.Error("Subscribers map should be initialized")
	}
	if s.Jobs == nil {
		t.Error("Jobs map should be initialized")
	}
	if len(s.Sessions) != 0 {
		t.Errorf("Sessions should be empty, got %d entries", len(s.Sessions))
	}
	if s.NextJobID != 0 {
		t.Errorf("NextJobID should be 0, got %d", s.NextJobID)
	}
	if s.NextConnID != 0 {
		t.Errorf("NextConnID should be 0, got %d", s.NextConnID)
	}
	if !s.Now.IsZero() {
		t.Errorf("Now should be zero, got %v", s.Now)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	for _, st := range []Status{StatusRunning, StatusWaiting, StatusIdle, StatusStopped, StatusPending} {
		got, ok := ParseStatus(st.String())
		if !ok {
			t.Errorf("ParseStatus(%q) returned ok=false", st.String())
			continue
		}
		if got != st {
			t.Errorf("ParseStatus(%q) = %v, want %v", st.String(), got, st)
		}
	}
}

func TestParseStatusUnknown(t *testing.T) {
	if _, ok := ParseStatus(""); ok {
		t.Error("ParseStatus(\"\") should return ok=false")
	}
	if _, ok := ParseStatus("garbage"); ok {
		t.Error("ParseStatus(\"garbage\") should return ok=false")
	}
}

func TestStatusInfoZeroValue(t *testing.T) {
	var info StatusInfo
	if info.Status != StatusRunning {
		// StatusRunning == 0 by iota; the zero value of an int is 0,
		// so a zero StatusInfo reads as Running. Document the trap.
		t.Errorf("zero StatusInfo.Status = %v, want StatusRunning (iota base)", info.Status)
	}
	if !info.ChangedAt.IsZero() {
		t.Error("zero StatusInfo.ChangedAt should be zero time")
	}
}

func TestCloneSessionsIndependence(t *testing.T) {
	orig := map[SessionID]Session{
		"a": {ID: "a", Project: "/foo"},
		"b": {ID: "b", Project: "/bar"},
	}
	cloned := cloneSessions(orig)
	if len(cloned) != 2 {
		t.Fatalf("clone len = %d, want 2", len(cloned))
	}
	cloned["c"] = Session{ID: "c", Project: "/baz"}
	if _, ok := orig["c"]; ok {
		t.Error("mutating clone leaked into original")
	}
	if len(orig) != 2 {
		t.Errorf("orig len = %d, want 2 (clone mutation leaked)", len(orig))
	}
}

func TestReduceEmptyTickEmitsHealthChecks(t *testing.T) {
	now := time.Now()
	s := New()
	next, effs := Reduce(s, EvTick{Now: now})
	if !next.Now.Equal(now) {
		t.Errorf("Now = %v, want %v", next.Now, now)
	}
	// 3 EffCheckPaneAlive: 0.1 every tick + 0.0/0.2 every 5 ticks (N=0 fires).
	// (0.3 removed: log pane no longer has a fixed position)
	// + 1 EffReconcileWindows. No broadcast/persist when no sessions changed.
	var checks int
	for _, e := range effs {
		if _, ok := e.(EffCheckPaneAlive); ok {
			checks++
		}
	}
	if checks != 3 {
		t.Errorf("EffCheckPaneAlive count = %d, want 3", checks)
	}
}
