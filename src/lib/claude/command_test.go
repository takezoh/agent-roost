package claude

import "testing"

func TestCurrentRoostSessionID_FromEnv(t *testing.T) {
	t.Setenv("ROOST_SESSION_ID", "abc123")
	id, ok := currentRoostSessionID()
	if !ok {
		t.Fatalf("currentRoostSessionID() ok = false, want true when ROOST_SESSION_ID is set")
	}
	if id != "abc123" {
		t.Errorf("currentRoostSessionID() id = %q, want %q", id, "abc123")
	}
}

func TestCurrentRoostSessionID_Empty(t *testing.T) {
	t.Setenv("ROOST_SESSION_ID", "")
	id, ok := currentRoostSessionID()
	if ok {
		t.Errorf("currentRoostSessionID() = (%q, true), want ok=false when ROOST_SESSION_ID is unset", id)
	}
	if id != "" {
		t.Errorf("currentRoostSessionID() id = %q, want empty", id)
	}
}
