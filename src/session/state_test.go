package session

import "testing"

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
