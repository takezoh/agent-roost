package state

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/uiproc"
)

// stepActiveSessions delivers DEvTick to all sessions regardless of status.
// Drivers decide internally whether to react (self-skip via no-op return).
// These tests verify that the runtime does NOT gate on status.
func TestTickDeliversToDAllSessions(t *testing.T) {
	now := time.Now()
	for _, status := range []Status{StatusIdle, StatusStopped, StatusRunning, StatusWaiting} {
		s := New()
		s.Sessions["s1"] = Session{
			ID:      "s1",
			Command: "stub",
			Driver:  stubDriverState{status: status},
		}
		next, _ := Reduce(s, EvTick{Now: now})
		// stubDriver.Step is a no-op, so state is unchanged — just confirm no panic.
		if next.Now != now {
			t.Errorf("status=%v: expected Now to be updated", status)
		}
	}
}

func TestTickProcessesRunningSessions(t *testing.T) {
	now := time.Now()
	s := New()
	s.Sessions["run1"] = Session{
		ID:      "run1",
		Command: "stub",
		Driver:  stubDriverState{status: StatusRunning},
	}
	s.ActiveSession = "run1"

	_, effs := Reduce(s, EvTick{Now: now})

	// Should have reconcile + health checks at minimum
	var reconcile int
	for _, e := range effs {
		if _, ok := e.(EffReconcileWindows); ok {
			reconcile++
		}
	}
	if reconcile != 1 {
		t.Errorf("EffReconcileWindows count = %d, want 1", reconcile)
	}
}

func TestTickInitializesConnectors(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "test"})

	s := New()
	next, _ := Reduce(s, EvTick{Now: time.Now()})
	if !next.ConnectorsReady {
		t.Error("ConnectorsReady should be true after first tick")
	}
	if _, ok := next.Connectors["test"]; !ok {
		t.Error("Connector 'test' should be initialized")
	}
}

func TestTickInitializesConnectorsOnlyOnce(t *testing.T) {
	orig := connectorRegistry
	connectorRegistry = map[string]Connector{}
	defer func() { connectorRegistry = orig }()

	RegisterConnector(stubConnector{name: "test"})

	s := New()
	s.ConnectorsReady = true
	s.Connectors["test"] = stubConnectorState{Val: 42}

	next, _ := Reduce(s, EvTick{Now: time.Now()})
	cs := next.Connectors["test"].(stubConnectorState)
	// Should Step (Val incremented) but not re-initialize (Val would be 0).
	if cs.Val != 43 {
		t.Errorf("Val = %d, want 43 (Step once, not re-initialized)", cs.Val)
	}
}

func TestPaneDiedActiveSessionEmitsDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = stubSession(id)
	s.ActiveSession = id
	_, effs := Reduce(s, EvPaneDied{Pane: "{sessionName}:0.1", OwnerFrameID: FrameID(id)})
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when active session's pane dies")
	}
}

func TestTmuxWindowVanishedActiveSessionEmitsDeactivateAndRespawn(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = stubSession(id)
	s.ActiveSession = id
	_, effs := Reduce(s, EvTmuxWindowVanished{FrameID: FrameID(id)})
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when active session's window vanishes")
	}
	if _, ok := findEff[EffRespawnPane](effs); ok {
		t.Error("should not respawn pane 0.0 directly after active session window vanishes")
	}
}

func TestTmuxWindowVanishedInactiveSessionNoDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	other := SessionID("other")
	s.Sessions[id] = stubSession(id)
	s.ActiveSession = other
	_, effs := Reduce(s, EvTmuxWindowVanished{FrameID: FrameID(id)})
	if _, ok := findEff[EffDeactivateSession](effs); ok {
		t.Error("should not emit EffDeactivateSession for inactive session's window vanish")
	}
}

// === sibling independence (new model) ===

// TestSiblingIndependence verifies that evicting a child frame leaves
// all other frames (siblings and root) intact.
func TestSiblingIndependence(t *testing.T) {
	s := New()
	id := SessionID("abc")
	rootID := FrameID("frame-root")
	child1ID := FrameID("frame-child1")
	child2ID := FrameID("frame-child2")
	s.Sessions[id] = Session{
		ID:      id,
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
		Frames: []SessionFrame{
			{ID: rootID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
			{ID: child1ID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
			{ID: child2ID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
		},
	}
	s.ActiveSession = id

	next, _ := Reduce(s, EvTmuxWindowVanished{FrameID: child1ID})

	sess, ok := next.Sessions[id]
	if !ok {
		t.Fatal("session should remain when root frame survives")
	}
	if len(sess.Frames) != 2 {
		t.Fatalf("frames = %d, want 2 (root + child2)", len(sess.Frames))
	}
	found := false
	for _, f := range sess.Frames {
		if f.ID == child2ID {
			found = true
		}
		if f.ID == child1ID {
			t.Errorf("evicted child1 should not appear in frames")
		}
	}
	if !found {
		t.Error("child2 should survive after child1 is evicted")
	}
	if sess.Frames[0].ID != rootID {
		t.Errorf("root frame should survive, got %q", sess.Frames[0].ID)
	}
}

// TestPaneDiedTopFrameReactivateBeforeKill asserts Fix A: when the active top
// frame's pane dies, EffActivateSession (restore parent to 0.1) must precede
// EffKillSessionWindow (tear down the top frame's window).
// Reversing the order causes kill-window to destroy window 0.
func TestPaneDiedTopFrameReactivateBeforeKill(t *testing.T) {
	s := New()
	id := SessionID("sess-pop")
	rootID := FrameID("frame-root")
	topID := FrameID("frame-top")
	s.Sessions[id] = Session{
		ID:            id,
		Project:       "/project",
		Command:       "stub",
		Driver:        stubDriverState{},
		ActiveFrameID: topID,
		Frames: []SessionFrame{
			{ID: rootID, Project: "/project", Command: "stub", Driver: stubDriverState{}},
			{ID: topID, Project: "/project", Command: "stub", Driver: stubDriverState{}},
		},
	}
	s.ActiveSession = id

	next, effs := Reduce(s, EvPaneDied{Pane: "{sessionName}:0.1", OwnerFrameID: topID})

	activateIdx := -1
	killIdx := -1
	for i, e := range effs {
		if _, ok := e.(EffActivateSession); ok {
			activateIdx = i
		}
		if ks, ok := e.(EffKillSessionWindow); ok && ks.FrameID == topID {
			killIdx = i
		}
	}
	if activateIdx < 0 {
		t.Fatal("expected EffActivateSession")
	}
	if killIdx < 0 {
		t.Fatal("expected EffKillSessionWindow for top frame")
	}
	if activateIdx > killIdx {
		t.Errorf("EffActivateSession (idx %d) must precede EffKillSessionWindow (idx %d)", activateIdx, killIdx)
	}

	// Verify state: root frame survives, session stays active.
	sess, ok := next.Sessions[id]
	if !ok {
		t.Fatal("session should survive when root frame remains")
	}
	if len(sess.Frames) != 1 || sess.Frames[0].ID != rootID {
		t.Errorf("frames = %v, want [root]", sess.Frames)
	}
	if next.ActiveSession != id {
		t.Errorf("ActiveSession = %q, want %q", next.ActiveSession, id)
	}
}

// TestMRUFallbackOnFrameDeath verifies that when the active child frame dies,
// the previously active frame (via MRU) becomes the new active frame.
func TestMRUFallbackOnFrameDeath(t *testing.T) {
	s := New()
	id := SessionID("sess-mru")
	rootID := FrameID("frame-root")
	child1ID := FrameID("frame-child1")
	child2ID := FrameID("frame-child2")
	s.Sessions[id] = Session{
		ID:      id,
		Project: "/foo",
		Command: "stub",
		Driver:  stubDriverState{},
		Frames: []SessionFrame{
			{ID: rootID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
			{ID: child1ID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
			{ID: child2ID, Project: "/foo", Command: "stub", Driver: stubDriverState{}},
		},
		ActiveFrameID: child2ID,
		MRUFrameIDs:   []FrameID{child1ID, rootID},
	}
	s.ActiveSession = id

	next, _ := Reduce(s, EvTmuxWindowVanished{FrameID: child2ID})

	sess, ok := next.Sessions[id]
	if !ok {
		t.Fatal("session should survive child2 death")
	}
	if sess.ActiveFrameID != child1ID {
		t.Errorf("ActiveFrameID = %q, want child1 via MRU fallback", sess.ActiveFrameID)
	}
	for _, f := range sess.Frames {
		if f.ID == child2ID {
			t.Error("dead child2 should not appear in frames")
		}
	}
}

func TestTickNoBroadcastWhenNoChange(t *testing.T) {
	now := time.Now()
	s := New()
	// Running session but stubDriver.Step returns same state + no effects
	s.Sessions["s1"] = Session{
		ID:      "s1",
		Command: "stub",
		Driver:  stubDriverState{status: StatusRunning},
		Frames: []SessionFrame{{
			ID:      "s1",
			Command: "stub",
			Driver:  stubDriverState{status: StatusRunning},
		}},
	}

	_, effs := Reduce(s, EvTick{Now: now})

	for _, e := range effs {
		if _, ok := e.(EffBroadcastSessionsChanged); ok {
			t.Error("should not broadcast when no driver state changed")
		}
		if _, ok := e.(EffPersistSnapshot); ok {
			t.Error("should not persist when no driver state changed")
		}
	}
}

// TestHiddenPaneDiedRestartsLogTUI verifies that a EvPaneDied for the hidden
// pane emits EffRespawnPane targeting the log TUI process.
func TestHiddenPaneDiedRestartsLogTUI(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvPaneDied{Pane: "{sessionName}:__hidden__.0"})

	respawn, ok := findEff[EffRespawnPane](effs)
	if !ok {
		t.Fatal("expected EffRespawnPane for hidden pane death")
	}
	if respawn.Pane != "{sessionName}:__hidden__.0" {
		t.Errorf("respawn target = %q, want __hidden__.0", respawn.Pane)
	}
	want := uiproc.Log()
	if respawn.Proc.Name != want.Name {
		t.Errorf("respawn proc = %q, want %q", respawn.Proc.Name, want.Name)
	}
}

// TestHiddenPaneHealthCheckOnTickN0 verifies that the tick emits
// EffCheckPaneAlive for the __hidden__ pane when N%5==0.
func TestHiddenPaneHealthCheckOnTickN0(t *testing.T) {
	s := New()
	_, effs := Reduce(s, EvTick{Now: time.Now(), N: 0})

	var hiddenChecks int
	for _, e := range effs {
		if c, ok := e.(EffCheckPaneAlive); ok {
			if c.Pane == "{sessionName}:__hidden__.0" {
				hiddenChecks++
			}
		}
	}
	if hiddenChecks != 1 {
		t.Errorf("hidden pane EffCheckPaneAlive count = %d, want 1 on tick N=0", hiddenChecks)
	}
}
