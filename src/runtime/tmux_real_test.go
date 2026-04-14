package runtime

import (
	"fmt"
	"testing"
)

// TestGuardNotMainWindowRefusesIndex0 verifies Fix B: guardNotMainWindow returns
// an error when the target pane is in tmux window index 0 (the main layout).
func TestGuardNotMainWindowRefusesIndex0(t *testing.T) {
	err := guardNotMainWindow("roost:0.0", func(_, _ string) (string, error) {
		return "0", nil
	})
	if err == nil {
		t.Fatal("expected error when window index is 0")
	}
}

// TestGuardNotMainWindowAllowsNonZero verifies that guardNotMainWindow allows
// panes in non-main windows (index > 0).
func TestGuardNotMainWindowAllowsNonZero(t *testing.T) {
	for _, idx := range []string{"1", "2", "9"} {
		t.Run("index="+idx, func(t *testing.T) {
			err := guardNotMainWindow("roost:"+idx+".0", func(_, _ string) (string, error) {
				return idx, nil
			})
			if err != nil {
				t.Errorf("unexpected error for index %s: %v", idx, err)
			}
		})
	}
}

// TestGuardNotMainWindowPropagatesDisplayError verifies that display-message
// errors are forwarded unchanged.
func TestGuardNotMainWindowPropagatesDisplayError(t *testing.T) {
	want := fmt.Errorf("tmux: no such pane")
	err := guardNotMainWindow("ghost:0.0", func(_, _ string) (string, error) {
		return "", want
	})
	if err != want {
		t.Errorf("err = %v, want %v", err, want)
	}
}
