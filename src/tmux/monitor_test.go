package tmux

import (
	"errors"
	"testing"
	"time"

	"github.com/take/agent-roost/session"
	"github.com/take/agent-roost/session/driver"
)

type mockCapturer struct {
	content map[string]string
	err     error
}

func (m *mockCapturer) CapturePaneLines(target string, n int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.content[target], nil
}

func TestDetectStateRunning(t *testing.T) {
	m := NewMonitor(&mockCapturer{content: map[string]string{"@1.0": "compiling..."}}, 60, nil)
	if got := m.DetectState("@1", "bash"); got != session.StateRunning {
		t.Fatalf("got %v, want StateRunning", got)
	}
}

func TestDetectStateWaiting(t *testing.T) {
	m := NewMonitor(&mockCapturer{content: map[string]string{"@1.0": "some output\n$ "}}, 60, nil)
	if got := m.DetectState("@1", "bash"); got != session.StateWaiting {
		t.Fatalf("got %v, want StateWaiting", got)
	}
}

func TestDetectStateIdle(t *testing.T) {
	m := NewMonitor(&mockCapturer{content: map[string]string{"@1.0": "done\n$ "}}, 0, nil)
	m.DetectState("@1", "bash")
	if got := m.DetectState("@1", "bash"); got != session.StateIdle {
		t.Fatalf("got %v, want StateIdle", got)
	}
}

func TestDetectStateStopped(t *testing.T) {
	m := NewMonitor(&mockCapturer{err: errors.New("dead")}, 60, nil)
	if got := m.DetectState("@1", "bash"); got != session.StateStopped {
		t.Fatalf("got %v, want StateStopped", got)
	}
}

func TestDetectState_PreservesRunning(t *testing.T) {
	cap := &mockCapturer{content: map[string]string{"@1.0": "compiling..."}}
	m := NewMonitor(cap, 30, nil)
	m.DetectState("@1", "bash")
	if got := m.DetectState("@1", "bash"); got != session.StateRunning {
		t.Fatalf("got %v, want Running (preserved)", got)
	}
}

func TestPollAll(t *testing.T) {
	cap := &mockCapturer{content: map[string]string{
		"@1.0": "compiling...",
		"@2.0": "output\n$ ",
	}}
	m := NewMonitor(cap, 60, nil)
	states := m.PollAll(map[string]string{"@1": "bash", "@2": "bash"})
	if len(states) != 2 {
		t.Fatalf("got %d states, want 2", len(states))
	}
	if states["@1"] != session.StateRunning {
		t.Errorf("@1: got %v, want StateRunning", states["@1"])
	}
	if states["@2"] != session.StateWaiting {
		t.Errorf("@2: got %v, want StateWaiting", states["@2"])
	}
}

func TestExtractCost(t *testing.T) {
	m := NewMonitor(&mockCapturer{content: map[string]string{"@1.0": "Total: $1.23"}}, 60, nil)
	if got := m.ExtractCost("@1"); got != "$1.23" {
		t.Fatalf("got %q, want %q", got, "$1.23")
	}

	m2 := NewMonitor(&mockCapturer{err: errors.New("fail")}, 60, nil)
	if got := m2.ExtractCost("@1"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestHasPromptIndicator(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"$ ", true},
		{"> ", true},
		{"❯ ", true},
		{"hello", false},
		{">", true},
		{"", false},
		{"line1\nline2\n$ ", true},
	}
	for _, tt := range tests {
		if got := hasPromptIndicator(tt.input, nil); got != tt.want {
			t.Errorf("hasPromptIndicator(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestComputeTransition_NewContent_NoPrompt(t *testing.T) {
	now := time.Now()
	state, snap := computeTransition("compiling...", snapshot{}, now, 30*time.Second, nil)
	if state != session.StateRunning {
		t.Fatalf("got %v, want Running", state)
	}
	if snap.lastState != session.StateRunning {
		t.Fatalf("snap state: got %v, want Running", snap.lastState)
	}
}

func TestComputeTransition_NewContent_WithPrompt(t *testing.T) {
	now := time.Now()
	state, snap := computeTransition("done\n$ ", snapshot{}, now, 30*time.Second, nil)
	if state != session.StateWaiting {
		t.Fatalf("got %v, want Waiting", state)
	}
	if snap.lastState != session.StateWaiting {
		t.Fatalf("snap state: got %v, want Waiting", snap.lastState)
	}
}

func TestComputeTransition_UnchangedWithinThreshold(t *testing.T) {
	now := time.Now()
	_, snap := computeTransition("compiling...", snapshot{}, now, 30*time.Second, nil)
	state, _ := computeTransition("compiling...", snap, now.Add(5*time.Second), 30*time.Second, nil)
	if state != session.StateRunning {
		t.Fatalf("got %v, want Running (preserved)", state)
	}
}

func TestComputeTransition_UnchangedExceedsThreshold(t *testing.T) {
	now := time.Now()
	_, snap := computeTransition("compiling...", snapshot{}, now, 30*time.Second, nil)
	state, _ := computeTransition("compiling...", snap, now.Add(31*time.Second), 30*time.Second, nil)
	if state != session.StateIdle {
		t.Fatalf("got %v, want Idle", state)
	}
}

func TestComputeTransition_PreservesWaiting(t *testing.T) {
	now := time.Now()
	_, snap := computeTransition("done\n$ ", snapshot{}, now, 30*time.Second, nil)
	state, _ := computeTransition("done\n$ ", snap, now.Add(5*time.Second), 30*time.Second, nil)
	if state != session.StateWaiting {
		t.Fatalf("got %v, want Waiting (preserved)", state)
	}
}

func TestDetectState_ClaudePattern_DollarNotPrompt(t *testing.T) {
	// In the claude pattern, $ is not recognized as a prompt
	registry := driver.DefaultRegistry()
	cap := &mockCapturer{content: map[string]string{"@1.0": "output\n$ "}}
	m := NewMonitor(cap, 60, registry)
	if got := m.DetectState("@1", "claude"); got != session.StateRunning {
		t.Fatalf("claude: got %v, want StateRunning ($ should not match claude pattern)", got)
	}
}

func TestDetectState_ClaudePattern_ArrowPrompt(t *testing.T) {
	// In the claude pattern, ❯ is recognized as a prompt
	registry := driver.DefaultRegistry()
	cap := &mockCapturer{content: map[string]string{"@1.0": "output\n❯ "}}
	m := NewMonitor(cap, 60, registry)
	if got := m.DetectState("@1", "claude"); got != session.StateWaiting {
		t.Fatalf("claude: got %v, want StateWaiting (❯ should match claude pattern)", got)
	}
}
