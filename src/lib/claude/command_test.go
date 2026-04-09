package claude

import "testing"

func TestCurrentRoostPane_NoTmuxEnv(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	pane, ok := currentRoostPane()
	if ok {
		t.Errorf("currentRoostPane() = (%q, true), want ok=false when TMUX_PANE is unset", pane)
	}
	if pane != "" {
		t.Errorf("currentRoostPane() pane = %q, want empty", pane)
	}
}
