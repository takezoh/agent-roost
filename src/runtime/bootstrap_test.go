package runtime

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver"
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
	r.sessionPanes["_main"] = "%main"

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

func TestRecoverWarmStartSessions_ReinstallsTranscriptWatch(t *testing.T) {
	watcher := &recordingWatcher{}
	persist := &recordingPersist{}
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         newFakeTmux(),
		Watcher:      watcher,
		Persist:      persist,
	})
	now := time.Date(2026, 4, 13, 0, 0, 0, 0, time.UTC)
	d := driver.NewCodexDriver("/tmp/events")
	r.state.Sessions["s1"] = state.Session{
		ID:        "s1",
		Project:   "/repo",
		Command:   "codex",
		CreatedAt: now,
		Driver: d.Restore(map[string]string{
			"transcript_path":  "/tmp/t.jsonl",
			"codex_session_id": "sess-1",
		}, now),
	}

	r.RecoverWarmStartSessions()

	watcher.mu.Lock()
	gotPath := watcher.watches["s1"]
	watcher.mu.Unlock()
	if gotPath != "/tmp/t.jsonl" {
		t.Fatalf("watch path = %q, want /tmp/t.jsonl", gotPath)
	}
	if len(r.state.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(r.state.Jobs))
	}
	got := r.state.Sessions["s1"].Driver.(driver.CodexState)
	if !got.TranscriptInFlight {
		t.Fatal("TranscriptInFlight should be true")
	}
	if persist.saves == 0 {
		t.Fatal("expected persist on rehydrate")
	}
}

func TestRecoverActivePaneAtMain_RestoresMainTUIWhenSessionActive(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.mu.Lock()
	ftmux.spawnPane = "%2"
	ftmux.mu.Unlock()

	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1", Project: "/repo/project"}
	r.sessionPanes["s1"] = "%2"
	r.sessionPanes["_main"] = "%1"

	r.RecoverActivePaneAtMain()

	if r.activeSession != "" {
		t.Errorf("activeSession = %q, want empty", r.activeSession)
	}
	if r.sessionPanes["_main"] != "%1" {
		t.Errorf("sessionPanes[_main] = %q, want %%1", r.sessionPanes["_main"])
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.swapCalls != 1 {
		t.Fatalf("swapCalls = %d, want 1", ftmux.swapCalls)
	}
	if ftmux.swapSources[0] != "%1" || ftmux.swapTargets[0] != "roost-test:0.0" {
		t.Fatalf("swap = %q -> %q, want %%1 -> roost-test:0.0", ftmux.swapSources[0], ftmux.swapTargets[0])
	}
}

func TestRecoverActivePaneAtMain_IdentifiesMainTUIActive(t *testing.T) {
	ftmux := newFakeTmux()
	// 0.0 contains %1, which is the Main TUI
	ftmux.mu.Lock()
	ftmux.spawnPane = "%1"
	ftmux.mu.Unlock()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.sessionPanes["s1"] = "%2"
	r.sessionPanes["_main"] = "%1"

	r.RecoverActivePaneAtMain()

	if r.activeSession != "" {
		t.Errorf("activeSession = %q, want empty", r.activeSession)
	}
}

func TestRecoverActivePaneAtMain_LeavesSessionActiveWhenMainPaneUnknown(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.mu.Lock()
	ftmux.spawnPane = "%2"
	ftmux.mu.Unlock()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions["s1"] = state.Session{ID: "s1"}
	r.sessionPanes["s1"] = "%2"

	r.RecoverActivePaneAtMain()

	if r.activeSession != "s1" {
		t.Errorf("activeSession = %q, want s1", r.activeSession)
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.swapCalls != 0 {
		t.Errorf("swapCalls = %d, want 0", ftmux.swapCalls)
	}
}
