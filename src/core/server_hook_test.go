package core

import (
	"testing"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

func TestResolveAgentState_ClaudeWithHookState(t *testing.T) {
	agent := &driver.AgentSession{State: driver.AgentStateWaiting}
	got := ResolveAgentState("claude", session.StateRunning, agent)
	if got != session.StateWaiting {
		t.Errorf("got %v, want Waiting", got)
	}
}

func TestResolveAgentState_ClaudeUnsetDefaultsToIdle(t *testing.T) {
	agent := &driver.AgentSession{State: driver.AgentStateUnset}
	got := ResolveAgentState("claude", session.StateRunning, agent)
	if got != session.StateIdle {
		t.Errorf("got %v, want Idle", got)
	}
}

func TestResolveAgentState_ClaudeNilAgentDefaultsToIdle(t *testing.T) {
	got := ResolveAgentState("claude", session.StateRunning, nil)
	if got != session.StateIdle {
		t.Errorf("got %v, want Idle", got)
	}
}

func TestResolveAgentState_ClaudePending(t *testing.T) {
	agent := &driver.AgentSession{State: driver.AgentStatePending}
	got := ResolveAgentState("claude", session.StateWaiting, agent)
	if got != session.StatePending {
		t.Errorf("got %v, want Pending", got)
	}
}

func TestResolveAgentState_NonClaudeUsesCapturePane(t *testing.T) {
	agent := &driver.AgentSession{State: driver.AgentStateWaiting}
	got := ResolveAgentState("gemini", session.StateRunning, agent)
	if got != session.StateRunning {
		t.Errorf("got %v, want Running (capture-pane)", got)
	}
}
