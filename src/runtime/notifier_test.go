package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/config"
	"github.com/takezoh/agent-roost/state"
)

func TestConfigNotifierDispatch(t *testing.T) {
	baseEff := state.EffNotify{
		Driver:  "claude",
		Command: "claude",
		Project: "/home/user/prjA",
		Kind:    state.NotifyKindPendingApproval,
	}

	tests := []struct {
		name      string
		rules     []config.NotifyRule
		eff       state.EffNotify
		wantCalls int
	}{
		{
			name: "rule matches",
			rules: []config.NotifyRule{
				{Driver: "claude", Kind: "pending_approval"},
			},
			eff:       baseEff,
			wantCalls: 1,
		},
		{
			name: "rule does not match: driver mismatch",
			rules: []config.NotifyRule{
				{Driver: "codex", Kind: "pending_approval"},
			},
			eff:       baseEff,
			wantCalls: 0,
		},
		{
			name:      "no rules: no calls",
			rules:     nil,
			eff:       baseEff,
			wantCalls: 0,
		},
		{
			name: "project wildcard match",
			rules: []config.NotifyRule{
				{Project: "/home/user/*", Kind: "pending_approval"},
			},
			eff:       baseEff,
			wantCalls: 1,
		},
		{
			name: "project glob no match",
			rules: []config.NotifyRule{
				{Project: "/home/other/*"},
			},
			eff:       baseEff,
			wantCalls: 0,
		},
		{
			name: "done kind matching",
			rules: []config.NotifyRule{
				{Kind: "done"},
			},
			eff:       state.EffNotify{Driver: "x", Command: "x", Project: "/p", Kind: state.NotifyKindDone},
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callCount atomic.Int32
			cfg := &config.NotificationsConfig{Rules: tt.rules}
			n := &configNotifier{
				cfg: cfg,
				send: func(_ context.Context, _, _ string) error {
					callCount.Add(1)
					return nil
				},
			}
			n.Dispatch(tt.eff)
			// Allow goroutine to run
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				if int(callCount.Load()) >= tt.wantCalls {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if got := int(callCount.Load()); got != tt.wantCalls {
				t.Errorf("send called %d times, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestConfigNotifierDispatchOSC(t *testing.T) {
	tests := []struct {
		name      string
		rules     []config.NotifyRule
		source    string
		wantCalls int
	}{
		{
			name:      "no rules: OSC not toasted",
			rules:     nil,
			source:    "osc9",
			wantCalls: 0,
		},
		{
			name:      "source matches osc9",
			rules:     []config.NotifyRule{{Source: "osc9"}},
			source:    "osc9",
			wantCalls: 1,
		},
		{
			name:      "source mismatch: osc9 rule vs osc99 event",
			rules:     []config.NotifyRule{{Source: "osc9"}},
			source:    "osc99",
			wantCalls: 0,
		},
		{
			name:      "wildcard source matches all",
			rules:     []config.NotifyRule{{Source: "*"}},
			source:    "osc777",
			wantCalls: 1,
		},
		{
			name:      "empty source rule matches osc event",
			rules:     []config.NotifyRule{{}},
			source:    "osc9",
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callCount atomic.Int32
			cfg := &config.NotificationsConfig{Rules: tt.rules}
			n := &configNotifier{
				cfg: cfg,
				send: func(_ context.Context, _, _ string) error {
					callCount.Add(1)
					return nil
				},
			}
			n.DispatchOSC("title", "body", tt.source)
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				if int(callCount.Load()) >= tt.wantCalls {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if got := int(callCount.Load()); got != tt.wantCalls {
				t.Errorf("send called %d times, want %d", got, tt.wantCalls)
			}
		})
	}
}

func TestNotifyTitleBody(t *testing.T) {
	eff := state.EffNotify{
		Driver:  "claude",
		Command: "claude",
		Project: "/home/user/prjA",
		Kind:    state.NotifyKindPendingApproval,
	}
	title := notifyTitle(eff)
	body := notifyBody(eff)
	if title != "[claude] pending_approval" {
		t.Errorf("title = %q, want %q", title, "[claude] pending_approval")
	}
	if body != "/home/user/prjA  claude" {
		t.Errorf("body = %q, want %q", body, "/home/user/prjA  claude")
	}
}
