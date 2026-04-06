package session

import "testing"

func TestStateString(t *testing.T) {
	for _, tt := range []struct {
		s    State
		want string
	}{
		{StateRunning, "running"}, {StateWaiting, "waiting"},
		{StateIdle, "idle"}, {StateStopped, "stopped"}, {State(99), "unknown"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.String(); got != tt.want {
				t.Errorf("State(%d).String() = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

func TestStateSymbol(t *testing.T) {
	for _, tt := range []struct {
		s    State
		want string
	}{
		{StateRunning, "●"}, {StateWaiting, "◆"},
		{StateIdle, "○"}, {StateStopped, "■"}, {State(99), "?"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.Symbol(); got != tt.want {
				t.Errorf("State(%d).Symbol() = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

func TestSessionName(t *testing.T) {
	s := Session{Project: "/tmp/my-project"}
	if got := s.Name(); got != "my-project" {
		t.Errorf("Name() = %q, want %q", got, "my-project")
	}
}

func TestSessionDisplayCommand(t *testing.T) {
	if got := (&Session{Command: "build"}).DisplayCommand(); got != "build" {
		t.Errorf("DisplayCommand() = %q, want %q", got, "build")
	}
	if got := (&Session{}).DisplayCommand(); got != "idle" {
		t.Errorf("DisplayCommand() = %q, want %q", got, "idle")
	}
}
