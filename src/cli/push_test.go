package cli

import (
	"testing"
)

func TestRunPushMissingCommand(t *testing.T) {
	err := RunPush(nil)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestRunPushMissingSessionID(t *testing.T) {
	t.Setenv("ROOST_SESSION_ID", "")
	err := RunPush([]string{"claude"})
	if err == nil {
		t.Fatal("expected error when ROOST_SESSION_ID is empty")
	}
	if err.Error() == "" {
		t.Fatal("error message must not be empty")
	}
}
