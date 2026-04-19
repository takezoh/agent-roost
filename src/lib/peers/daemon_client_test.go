package peers

import (
	"testing"
)

func TestCallerFrameID_Set(t *testing.T) {
	t.Setenv("ROOST_FRAME_ID", "frame-abc")
	if got := callerFrameID(); got != "frame-abc" {
		t.Errorf("got %q, want %q", got, "frame-abc")
	}
}

func TestCallerFrameID_Unset(t *testing.T) {
	t.Setenv("ROOST_FRAME_ID", "")
	if got := callerFrameID(); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestDialDaemon_CustomSocket(t *testing.T) {
	t.Setenv("ROOST_SOCKET", "/nonexistent/roost.sock")
	_, err := dialDaemon()
	if err == nil {
		t.Fatal("expected error for nonexistent socket, got nil")
	}
}

func TestDialDaemon_HomeFallback(t *testing.T) {
	t.Setenv("ROOST_SOCKET", "")
	t.Setenv("HOME", t.TempDir())
	_, err := dialDaemon()
	// no daemon running — connection refused is expected; what matters is no panic
	if err == nil {
		t.Fatal("expected error when no daemon is running, got nil")
	}
}
