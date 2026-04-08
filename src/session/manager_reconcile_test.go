package session

import "testing"

func TestReconcileWindows_NoChanges(t *testing.T) {
	mgr, _ := setupManager(t)
	mgr.Create("/tmp/proj", "claude")

	removed, err := mgr.ReconcileWindows()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected no removals, got %v", removed)
	}
	if len(mgr.All()) != 1 {
		t.Fatalf("expected session to remain, got %d", len(mgr.All()))
	}
}

func TestReconcileWindows_OneRemoved(t *testing.T) {
	mgr, tmux := setupManager(t)
	sess, _ := mgr.Create("/tmp/proj", "claude")

	// Simulate the agent process exiting: tmux destroys the window so it
	// disappears from ListRoostWindows.
	delete(tmux.windows, sess.WindowID)

	removed, err := mgr.ReconcileWindows()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].ID != sess.ID || removed[0].WindowID != sess.WindowID {
		t.Fatalf("expected removal of %s, got %v", sess.ID, removed)
	}
	if len(mgr.All()) != 0 {
		t.Fatalf("expected empty session list, got %d", len(mgr.All()))
	}

	// Snapshot must reflect the removal.
	loaded, err := mgr.loadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 0 {
		t.Fatalf("expected snapshot to be empty after reconcile, got %d", len(loaded))
	}
}

func TestReconcileWindows_PreservesRuntimeFields(t *testing.T) {
	mgr, tmux := setupManager(t)
	sessKeep, _ := mgr.Create("/tmp/proj-a", "claude")
	tmux.nextWindowID = "@2"
	sessGone, _ := mgr.Create("/tmp/proj-b", "claude")

	mgr.UpdateStates(map[string]State{sessKeep.WindowID: StateWaiting})
	delete(tmux.windows, sessGone.WindowID)

	removed, err := mgr.ReconcileWindows()
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0].ID != sessGone.ID {
		t.Fatalf("expected one removal of %s, got %v", sessGone.ID, removed)
	}

	survivor := mgr.FindByID(sessKeep.ID)
	if survivor == nil {
		t.Fatal("expected surviving session to remain")
	}
	if survivor.State != StateWaiting {
		t.Fatalf("runtime State must be preserved, got %s", survivor.State)
	}
}
