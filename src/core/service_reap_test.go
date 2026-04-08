package core

import "testing"

func TestReapDeadSessions(t *testing.T) {
	svc, _, mgr, mt := setupServiceWithTmux(t)

	// Create a session and bind an agent so we can verify Unbind is called.
	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.AgentStore.Bind(sess.WindowID, "agent-1")
	svc.Preview(sess)
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatal("setup: expected active window")
	}

	// First reap: nothing has died yet.
	if reaped := svc.ReapDeadSessions(); len(reaped) != 0 {
		t.Fatalf("expected no reap, got %v", reaped)
	}

	// Simulate the agent process exiting: tmux destroys the window.
	delete(mt.windows, sess.WindowID)

	reaped := svc.ReapDeadSessions()
	if len(reaped) != 1 || reaped[0] != sess.ID {
		t.Fatalf("expected reap of %s, got %v", sess.ID, reaped)
	}
	if len(mgr.All()) != 0 {
		t.Fatalf("expected empty session list, got %d", len(mgr.All()))
	}
	if svc.ActiveWindowID() != "" {
		t.Fatalf("expected ClearActive to fire, got %s", svc.ActiveWindowID())
	}
	if svc.AgentStore.GetByWindow(sess.WindowID) != nil {
		t.Fatal("expected AgentStore.Unbind to fire")
	}
}

func TestReapActiveDead(t *testing.T) {
	svc, panes, mgr, mt := setupServiceWithTmux(t)
	panes.paneDead = make(map[string]bool)
	panes.paneIDAt = make(map[string]string)

	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.AgentStore.Bind(sess.WindowID, "agent-1")
	if err := svc.Preview(sess); err != nil {
		t.Fatal(err)
	}
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatal("setup: expected active window")
	}

	// Simulate the agent dying inside pane 0.0: pane 0.0 is now dead and
	// holds the session's agent pane id.
	panes.paneDead["roost:0.0"] = true
	panes.paneIDAt["roost:0.0"] = sess.AgentPaneID

	reaped := svc.ReapDeadSessions()
	if len(reaped) != 1 || reaped[0] != sess.ID {
		t.Fatalf("expected reap of %s, got %v", sess.ID, reaped)
	}
	if svc.ActiveWindowID() != "" {
		t.Fatalf("expected active to be cleared, got %s", svc.ActiveWindowID())
	}
	if _, ok := mt.windows[sess.WindowID]; ok {
		t.Fatalf("expected session window to be killed")
	}
	if svc.AgentStore.GetByWindow(sess.WindowID) != nil {
		t.Fatal("expected AgentStore.Unbind to fire")
	}
	// Last RunChain should be the swap-pane back to the session window.
	if len(panes.chainCalls) == 0 || panes.chainCalls[0][0] != "swap-pane" {
		t.Fatalf("expected swap-pane chain, got %v", panes.chainCalls)
	}
}

// When pane 0.0 is dead but its pane id matches no session (e.g. the main
// TUI itself died), the reaper must leave everything alone — that is the
// health monitor's job.
func TestReapActiveDead_NoMatchingPane(t *testing.T) {
	svc, panes, mgr, mt := setupServiceWithTmux(t)
	panes.paneDead = map[string]bool{"roost:0.0": true}
	panes.paneIDAt = map[string]string{"roost:0.0": "%999"} // unknown pane id

	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)

	reaped := svc.ReapDeadSessions()
	if len(reaped) != 0 {
		t.Fatalf("expected no reap for unknown dead pane, got %v", reaped)
	}
	if _, ok := mt.windows[sess.WindowID]; !ok {
		t.Fatal("expected session window to remain")
	}
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatalf("expected active to remain, got %s", svc.ActiveWindowID())
	}
	if len(mgr.All()) != 1 {
		t.Fatalf("expected session to remain, got %d", len(mgr.All()))
	}
}

func TestReapActiveDead_NoActive(t *testing.T) {
	svc, panes, _, _ := setupServiceWithTmux(t)
	panes.paneDead = map[string]bool{"roost:0.0": true}
	panes.paneIDAt = map[string]string{"roost:0.0": "%42"}

	// No active session set: this could be the main TUI dying. The reaper
	// finds no session matching the dead pane id and leaves it alone (the
	// health monitor handles main TUI respawn).
	reaped := svc.ReapDeadSessions()
	if len(reaped) != 0 {
		t.Fatalf("expected no reap when no active session, got %v", reaped)
	}
}

func TestReapActiveDead_AlivePane00(t *testing.T) {
	svc, panes, mgr, _ := setupServiceWithTmux(t)
	panes.paneDead = map[string]bool{"roost:0.0": false}
	panes.paneIDAt = map[string]string{"roost:0.0": ""}

	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.Preview(sess)
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatal("setup: expected active window")
	}

	reaped := svc.ReapDeadSessions()
	if len(reaped) != 0 {
		t.Fatalf("expected no reap for alive pane, got %v", reaped)
	}
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatalf("expected active to remain, got %s", svc.ActiveWindowID())
	}
	if len(mgr.All()) != 1 {
		t.Fatalf("expected session to remain, got %d", len(mgr.All()))
	}
}

// Regression for the "wrong card disappears" bug: pane 0.0 holds session A's
// dead pane (the actual agent that died), but s.activeWindowID has drifted
// to point at session B (e.g. due to a concurrent Preview race). The reaper
// must use the dead pane id to identify A — NOT activeWindowID — so that
// only A's window is destroyed and B stays untouched.
func TestReapActiveDead_PaneSwappedOutFromActive(t *testing.T) {
	svc, panes, mgr, mt := setupServiceWithTmux(t)
	panes.paneDead = make(map[string]bool)
	panes.paneIDAt = make(map[string]string)

	// Create session A. Set the next window/pane id pair before the second
	// Create so B gets distinct identifiers from A.
	a, _ := mgr.Create("/tmp/proj", "claude")
	mt.nextID = "@2"
	mt.nextPaneID = "%2"
	b, _ := mgr.Create("/tmp/proj", "claude")
	if a.AgentPaneID == "" || b.AgentPaneID == "" || a.AgentPaneID == b.AgentPaneID {
		t.Fatalf("setup: expected distinct pane ids, got A=%q B=%q", a.AgentPaneID, b.AgentPaneID)
	}

	svc.AgentStore.Bind(a.WindowID, "agent-a")
	svc.AgentStore.Bind(b.WindowID, "agent-b")

	// Simulate "user previewed B last" — Service.activeWindowID points at B.
	// But pane 0.0 actually holds A's dead pane, mimicking the swap-pane race.
	if err := svc.Preview(b); err != nil {
		t.Fatal(err)
	}
	if svc.ActiveWindowID() != b.WindowID {
		t.Fatalf("setup: expected active=B, got %s", svc.ActiveWindowID())
	}
	panes.paneDead["roost:0.0"] = true
	panes.paneIDAt["roost:0.0"] = a.AgentPaneID

	reaped := svc.ReapDeadSessions()

	// Only A must be reaped — not B.
	if len(reaped) != 1 || reaped[0] != a.ID {
		t.Fatalf("expected reap of A=%s, got %v", a.ID, reaped)
	}
	if _, ok := mt.windows[a.WindowID]; ok {
		t.Fatalf("expected A's window to be killed")
	}
	if _, ok := mt.windows[b.WindowID]; !ok {
		t.Fatalf("expected B's window to remain")
	}
	// activeWindowID must NOT be cleared: the reaped session is A, but the
	// user's selected session is B, so leave B as the active view.
	if svc.ActiveWindowID() != b.WindowID {
		t.Fatalf("expected active to remain B, got %s", svc.ActiveWindowID())
	}
}
