package runtime

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestLoadSessionPanes_ParsesEnvVars(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.envOutput = "ROOST_SESSION_session_abc=%11\nROOST_SESSION_session_def=%12\nSOME_OTHER=value\n"
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions[state.SessionID("session_abc")] = state.Session{ID: "session_abc"}
	r.state.Sessions[state.SessionID("session_def")] = state.Session{ID: "session_def"}

	if err := r.LoadSessionPanes(); err != nil {
		t.Fatalf("LoadSessionPanes: %v", err)
	}
	if r.sessionPanes[state.SessionID("session_abc")] != "%11" {
		t.Errorf("session_abc → %q, want %%11", r.sessionPanes[state.SessionID("session_abc")])
	}
	if r.sessionPanes[state.SessionID("session_def")] != "%12" {
		t.Errorf("session_def → %q, want %%12", r.sessionPanes[state.SessionID("session_def")])
	}
	if _, ok := r.sessionPanes[state.SessionID("value")]; ok {
		t.Error("non-ROOST_SESSION_ env should not be parsed")
	}
}

func TestLoadSessionPanes_NoEnvSupport(t *testing.T) {
	tmux := noopTmux{}
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	// Should not error — backend just doesn't support ShowEnvironment
	if err := r.LoadSessionPanes(); err != nil {
		t.Fatalf("LoadSessionPanes with noop tmux: %v", err)
	}
}

func TestReconcileOrphans_DropsSessionWithoutPane(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.state.Sessions["s2"] = state.Session{ID: "s2"}
	r.sessionPanes["s1"] = "%1"

	r.ReconcileOrphans()

	if _, ok := r.state.Sessions["s1"]; !ok {
		t.Error("s1 should be kept")
	}
	if _, ok := r.state.Sessions["s2"]; ok {
		t.Error("s2 should be dropped (no pane)")
	}
}

func TestReconcileOrphans_RemovesStalePaneEntry(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.sessionPanes["s1"] = "%1"
	r.sessionPanes["ghost"] = "%2"

	r.ReconcileOrphans()

	if _, ok := r.sessionPanes["ghost"]; ok {
		t.Error("stale pane entry should be removed")
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if _, ok := ftmux.envs["ROOST_SESSION_ghost"]; ok {
		t.Error("stale ROOST_SESSION_ghost env should be unset")
	}
}

func TestDeactivateBeforeExit_SwapsBack(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.sessionPanes["s1"] = "%1"
	r.activeSession = "s1"
	r.mainPaneID = "%main"

	r.DeactivateBeforeExit()

	if r.activeSession != "" {
		t.Errorf("activeSession = %q, want empty", r.activeSession)
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.swapCalls != 1 {
		t.Errorf("swapCalls = %d, want 1", ftmux.swapCalls)
	}
}

func TestDeactivateBeforeExit_NoActive(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})

	r.DeactivateBeforeExit()

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.breakCalls != 0 || ftmux.breakNewCalls != 0 || ftmux.joinCalls != 0 || ftmux.swapCalls != 0 {
		t.Errorf("unexpected pane move calls: break=%d breakNew=%d join=%d swap=%d",
			ftmux.breakCalls, ftmux.breakNewCalls, ftmux.joinCalls, ftmux.swapCalls)
	}
}
