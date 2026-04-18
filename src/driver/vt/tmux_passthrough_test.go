//go:build integration

package vt_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestTmuxOscPassthrough verifies which OSC sequences survive tmux's
// capture-pane -p -e pipeline.
//
// Expected results (hypothesis):
//   - OSC 9 (notification): NOT present — tmux drops ephemeral notification sequences.
//   - OSC 8 (hyperlink): present — tmux stores hyperlinks as cell attributes and
//     re-emits them via capture-pane -e.
//
// Run with: cd src && go test -tags=integration ./driver/vt/ -run TestTmuxOscPassthrough -v
func TestTmuxOscPassthrough(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not found in PATH")
	}

	session := "roost-osc-test"
	cleanup := func() { exec.Command("tmux", "kill-session", "-t", session).Run() } //nolint:errcheck
	cleanup()
	t.Cleanup(cleanup)

	run := func(args ...string) string {
		out, err := exec.Command("tmux", args...).Output()
		if err != nil {
			t.Fatalf("tmux %v: %v", args, err)
		}
		return string(out)
	}

	run("new-session", "-d", "-s", session, "-x", "200", "-y", "50", "bash", "--norc")

	pane := session + ":0.0"

	// Enable OSC 8 hyperlink forwarding (same as enableHyperlinkForward in coordinator).
	// Without this, tmux silently strips OSC 8 from pane cell data too.
	exec.Command("tmux", "set-option", "-s", "terminal-features", "+*:hyperlinks").Run() //nolint:errcheck

	// Send OSC 9 notification (iTerm2 / Windows Terminal).
	run("send-keys", "-t", pane, `printf '\x1b]9;osc9test\x1b\\'`, "Enter")
	// Send OSC 8 hyperlink so we have a positive control.
	run("send-keys", "-t", pane,
		`printf '\x1b]8;;https://example.com\x1b\\linktext\x1b]8;;\x1b\\'`, "Enter")

	time.Sleep(300 * time.Millisecond)

	content := run("capture-pane", "-p", "-e", "-t", pane, "-S", "-100")

	t.Logf("capture-pane output length: %d bytes", len(content))

	hasOSC9 := strings.Contains(content, "\x1b]9;")
	hasOSC8 := strings.Contains(content, "\x1b]8;")

	t.Logf("OSC 9 present in capture-pane output: %v  (expected: false)", hasOSC9)
	t.Logf("OSC 8 present in capture-pane output: %v  (expected: true)", hasOSC8)

	if hasOSC9 {
		t.Log("RESULT: hypothesis WRONG — tmux does forward OSC 9 via capture-pane; investigate vt emulator path")
	} else {
		t.Log("RESULT: hypothesis CONFIRMED — tmux drops OSC 9 before capture-pane; pipe-pane approach needed")
	}

	if !hasOSC8 {
		t.Error("OSC 8 hyperlink should survive capture-pane -e (positive control failed)")
	}
}
