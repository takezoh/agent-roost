package runtime

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

func TestLoadWindowMap_ParsesEnvVars(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.envOutput = "ROOST_W_1=session_abc\nROOST_W_2=session_def\nSOME_OTHER=value\n"
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions[state.SessionID("session_abc")] = state.Session{ID: "session_abc"}
	r.state.Sessions[state.SessionID("session_def")] = state.Session{ID: "session_def"}

	if err := r.LoadWindowMap(); err != nil {
		t.Fatalf("LoadWindowMap: %v", err)
	}
	if r.windowMap[state.SessionID("session_abc")] != "1" {
		t.Errorf("session_abc → %q, want 1", r.windowMap[state.SessionID("session_abc")])
	}
	if r.windowMap[state.SessionID("session_def")] != "2" {
		t.Errorf("session_def → %q, want 2", r.windowMap[state.SessionID("session_def")])
	}
	if _, ok := r.windowMap[state.SessionID("value")]; ok {
		t.Error("non-ROOST_W_ env should not be parsed")
	}
}

func TestLoadWindowMap_NoEnvSupport(t *testing.T) {
	tmux := noopTmux{}
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	// Should not error — backend just doesn't support ShowEnvironment
	if err := r.LoadWindowMap(); err != nil {
		t.Fatalf("LoadWindowMap with noop tmux: %v", err)
	}
}

func TestReconcileOrphans_DropsSessionWithoutWindow(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.state.Sessions["s2"] = state.Session{ID: "s2"}
	r.windowMap["s1"] = "1" // s1 has a window
	// s2 has no window entry → should be dropped

	r.ReconcileOrphans()

	if _, ok := r.state.Sessions["s1"]; !ok {
		t.Error("s1 should be kept")
	}
	if _, ok := r.state.Sessions["s2"]; ok {
		t.Error("s2 should be dropped (no window)")
	}
}

func TestReconcileOrphans_RemovesStaleWindowEntry(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.windowMap["s1"] = "1"
	r.windowMap["ghost"] = "2" // no matching session → stale

	r.ReconcileOrphans()

	if _, ok := r.windowMap["ghost"]; ok {
		t.Error("stale window entry should be removed")
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if _, ok := ftmux.envs["ROOST_W_2"]; ok {
		t.Error("stale ROOST_W_2 env should be unset")
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
	r.windowMap["s1"] = "1"
	r.activeSession = "s1"

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
	if ftmux.swapCalls != 0 {
		t.Errorf("swapCalls = %d, want 0", ftmux.swapCalls)
	}
}
