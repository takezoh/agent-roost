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

	sess, _ := mgr.Create("/tmp/proj", "claude")
	svc.AgentStore.Bind(sess.WindowID, "agent-1")
	if err := svc.Preview(sess); err != nil {
		t.Fatal(err)
	}
	if svc.ActiveWindowID() != sess.WindowID {
		t.Fatal("setup: expected active window")
	}

	// Simulate the agent dying inside pane 0.0 (window 0 has remain-on-exit on
	// so the pane lingers as [exited]).
	panes.paneDead["roost:0.0"] = true

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
	// Last RunChain should be the Deactivate swap-pane back to the session window.
	if len(panes.chainCalls) == 0 || panes.chainCalls[0][0] != "swap-pane" {
		t.Fatalf("expected Deactivate swap-pane chain, got %v", panes.chainCalls)
	}
}

func TestReapActiveDead_NoActive(t *testing.T) {
	svc, panes, _, _ := setupServiceWithTmux(t)
	panes.paneDead = map[string]bool{"roost:0.0": true}

	// No active session set: this could be the main TUI dying. The reaper
	// should not touch it (the health monitor handles main TUI respawn).
	reaped := svc.ReapDeadSessions()
	if len(reaped) != 0 {
		t.Fatalf("expected no reap when no active session, got %v", reaped)
	}
}

func TestReapActiveDead_AlivePane00(t *testing.T) {
	svc, panes, mgr, _ := setupServiceWithTmux(t)
	panes.paneDead = map[string]bool{"roost:0.0": false}

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
