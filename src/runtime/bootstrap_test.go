package runtime

import (
	"testing"
	"time"

	"github.com/takezoh/agent-roost/state"
)

type fakeEnvClient struct {
	envs map[string]string
}

func (f *fakeEnvClient) GetEnv(key string) (string, error) {
	return f.envs[key], nil
}

func TestDeactivateOnStartup_SwapsBack(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:       state.SessionID("s1"),
		WindowID: state.WindowID("@10"),
	}
	r.state.Active = state.WindowID("@10")

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@10"}}
	r.DeactivateOnStartup(env)

	if r.state.Active != "" {
		t.Errorf("expected Active to be empty, got %q", r.state.Active)
	}
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.swapCalls != 1 {
		t.Errorf("expected 1 swap call, got %d", tmux.swapCalls)
	}
	if _, ok := tmux.envs["ROOST_ACTIVE_WINDOW"]; ok {
		t.Error("expected ROOST_ACTIVE_WINDOW to be unset")
	}
}

func TestDeactivateOnStartup_NoActiveWindow(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})

	env := &fakeEnvClient{envs: map[string]string{}}
	r.DeactivateOnStartup(env)

	if r.state.Active != "" {
		t.Errorf("expected Active to be empty, got %q", r.state.Active)
	}
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.swapCalls != 0 {
		t.Errorf("expected 0 swap calls, got %d", tmux.swapCalls)
	}
}

func TestRescueActiveSession_WindowGone(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.spawnWID = "@20"
	ftmux.spawnPane = "%5"
	ftmux.alive["roost-test:0.0"] = true
	// displayMsgs does NOT contain "@10" → windowExists returns false

	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:       state.SessionID("s1"),
		Project:  "/tmp/proj",
		WindowID: state.WindowID("@10"),
	}

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@10"}}
	r.RescueActiveSession(env)

	sess := r.state.Sessions[state.SessionID("s1")]
	if sess.WindowID != "@20" {
		t.Errorf("WindowID = %q, want @20", sess.WindowID)
	}
	if sess.PaneID != "%5" {
		t.Errorf("PaneID = %q, want %%5", sess.PaneID)
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.spawnCalls != 1 {
		t.Errorf("spawnCalls = %d, want 1", ftmux.spawnCalls)
	}
	if ftmux.swapCalls != 1 {
		t.Errorf("swapCalls = %d, want 1", ftmux.swapCalls)
	}
}

func TestRescueActiveSession_WindowSurvives(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.displayMsgs["@10"] = "@10" // window exists

	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:       state.SessionID("s1"),
		WindowID: state.WindowID("@10"),
	}

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@10"}}
	r.RescueActiveSession(env)

	if r.state.Sessions[state.SessionID("s1")].WindowID != "@10" {
		t.Error("WindowID should be unchanged")
	}
	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.spawnCalls != 0 {
		t.Errorf("spawnCalls = %d, want 0", ftmux.spawnCalls)
	}
}

func TestRescueActiveSession_NoActiveWindow(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})

	env := &fakeEnvClient{envs: map[string]string{}}
	r.RescueActiveSession(env)

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.spawnCalls != 0 {
		t.Errorf("spawnCalls = %d, want 0", ftmux.spawnCalls)
	}
}

func TestRescueActiveSession_SessionDeletedFromJSON(t *testing.T) {
	ftmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	// No session with WindowID "@10"

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@10"}}
	r.RescueActiveSession(env)

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.spawnCalls != 0 {
		t.Errorf("spawnCalls = %d, want 0", ftmux.spawnCalls)
	}
}

func TestRescueActiveSession_AgentDead(t *testing.T) {
	ftmux := newFakeTmux()
	ftmux.alive["roost-test:0.0"] = false
	// displayMsgs does NOT contain "@10" → window gone

	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         ftmux,
	})
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:       state.SessionID("s1"),
		WindowID: state.WindowID("@10"),
	}

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@10"}}
	r.RescueActiveSession(env)

	ftmux.mu.Lock()
	defer ftmux.mu.Unlock()
	if ftmux.spawnCalls != 0 {
		t.Errorf("spawnCalls = %d, want 0 (agent dead)", ftmux.spawnCalls)
	}
}

func TestDeactivateOnStartup_StaleWindow(t *testing.T) {
	tmux := newFakeTmux()
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         tmux,
	})
	// Session exists but with a different window ID
	r.state.Sessions[state.SessionID("s1")] = state.Session{
		ID:       state.SessionID("s1"),
		WindowID: state.WindowID("@20"),
	}
	r.state.Active = state.WindowID("@99")

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@99"}}
	r.DeactivateOnStartup(env)

	// Should not swap because @99 doesn't match any session
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.swapCalls != 0 {
		t.Errorf("expected 0 swap calls for stale window, got %d", tmux.swapCalls)
	}
	// But should still clean up env and state
	if _, ok := tmux.envs["ROOST_ACTIVE_WINDOW"]; ok {
		t.Error("expected ROOST_ACTIVE_WINDOW to be unset")
	}
	if r.state.Active != "" {
		t.Errorf("expected Active to be empty, got %q", r.state.Active)
	}
}
