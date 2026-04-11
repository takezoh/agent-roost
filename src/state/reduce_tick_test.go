package state

import (
	"testing"
	"time"
)

func TestTickSkipsIdleSessions(t *testing.T) {
	now := time.Now()
	s := New()
	s.Sessions["idle1"] = Session{
		ID:      "idle1",
		Command: "stub",
		Driver:  stubDriverState{status: StatusIdle},
	}
	_, effs := Reduce(s, EvTick{Now: now})

	for _, e := range effs {
		if _, ok := e.(EffStartJob); ok {
			t.Error("should not emit EffStartJob for idle session")
		}
	}
}

func TestTickSkipsStoppedSessions(t *testing.T) {
	now := time.Now()
	s := New()
	s.Sessions["stopped1"] = Session{
		ID:      "stopped1",
		Command: "stub",
		Driver:  stubDriverState{status: StatusStopped},
	}
	_, effs := Reduce(s, EvTick{Now: now})

	for _, e := range effs {
		if _, ok := e.(EffStartJob); ok {
			t.Error("should not emit EffStartJob for stopped session")
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
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = id
	_, effs := Reduce(s, EvPaneDied{Pane: "{sessionName}:0.0", OwnerSessionID: id})
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when active session's pane dies")
	}
}

func TestTmuxWindowVanishedActiveSessionEmitsDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = id
	_, effs := Reduce(s, EvTmuxWindowVanished{SessionID: id})
	if _, ok := findEff[EffDeactivateSession](effs); !ok {
		t.Error("expected EffDeactivateSession when active session's window vanishes")
	}
}

func TestTmuxWindowVanishedInactiveSessionNoDeactivate(t *testing.T) {
	s := New()
	id := SessionID("abc")
	other := SessionID("other")
	s.Sessions[id] = Session{ID: id, Command: "stub", Driver: stubDriverState{}}
	s.ActiveSession = other
	_, effs := Reduce(s, EvTmuxWindowVanished{SessionID: id})
	if _, ok := findEff[EffDeactivateSession](effs); ok {
		t.Error("should not emit EffDeactivateSession for inactive session's window vanish")
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
