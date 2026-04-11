package runtime

import (
	"testing"
	"time"

	"github.com/take/agent-roost/state"
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

	env := &fakeEnvClient{envs: map[string]string{"ROOST_ACTIVE_WINDOW": "@99"}}
	r.DeactivateOnStartup(env)

	// Should not swap because @99 doesn't match any session
	tmux.mu.Lock()
	defer tmux.mu.Unlock()
	if tmux.swapCalls != 0 {
		t.Errorf("expected 0 swap calls for stale window, got %d", tmux.swapCalls)
	}
}
