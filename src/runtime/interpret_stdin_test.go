package runtime

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/takezoh/agent-roost/driver"
	"github.com/takezoh/agent-roost/state"
)

func TestWrapCommandWithStdinProducesBashC(t *testing.T) {
	input := []byte("hello world\n")
	cmd := wrapCommandWithStdin("claude", input)

	if !strings.HasPrefix(cmd, "bash -c ") {
		t.Errorf("wrapCommandWithStdin = %q, expected bash -c prefix", cmd)
	}
	if !strings.Contains(cmd, "claude") {
		t.Errorf("wrapped command does not contain original command: %q", cmd)
	}
	if !strings.Contains(cmd, "rm -f") {
		t.Errorf("wrapped command does not contain rm -f cleanup: %q", cmd)
	}
}

// TestSnapshotSessionsStripsInitialInput verifies that InitialInput is
// cleared before sessions are written to disk so sensitive stdin content
// is never persisted.
func TestSnapshotSessionsStripsInitialInput(t *testing.T) {
	r := New(Config{
		SessionName:  "roost-test",
		TickInterval: 10 * time.Second,
		Tmux:         newFakeTmux(),
		Persist:      noopPersist{},
	})
	sid := state.SessionID("s-stdin")
	r.state.Sessions[sid] = state.Session{
		ID:      sid,
		Project: "/repo",
		Command: "shell",
		Driver:  driver.NewGenericDriver("shell", "shell", 0).NewState(time.Now()),
		Frames: []state.SessionFrame{{
			ID:      state.FrameID("f1"),
			Project: "/repo",
			Command: "shell",
			Driver:  driver.NewGenericDriver("shell", "shell", 0).NewState(time.Now()),
			LaunchOptions: state.LaunchOptions{
				InitialInput: []byte("secret prompt"),
			},
		}},
	}

	snaps := r.snapshotSessions()
	if len(snaps) != 1 || len(snaps[0].Frames) != 1 {
		t.Fatalf("unexpected snapshot structure: %+v", snaps)
	}
	if snaps[0].Frames[0].LaunchOptions.InitialInput != nil {
		t.Errorf("InitialInput should be nil in snapshot, got %q",
			snaps[0].Frames[0].LaunchOptions.InitialInput)
	}
}

func TestWrapCommandWithStdinCreatesTempFile(t *testing.T) {
	input := []byte("prompt text")
	cmd := wrapCommandWithStdin("codex", input)

	// Verify the temp file was created and contains the input.
	// The pattern in the command is: ... < 'path'; ...
	// Extract the path by finding the redirect and parsing up to the quote.
	redirIdx := strings.Index(cmd, " < ")
	if redirIdx == -1 {
		t.Fatalf("no stdin redirect in command: %q", cmd)
	}
	rest := cmd[redirIdx+3:] // skip " < "
	// rest starts with the raw temp-file path (no inner shell-quoting because
	// CreateTemp paths are free of special shell characters).
	// The path ends at the first ';' or whitespace inside the bash -c string.
	end := strings.IndexAny(rest, "; '")
	if end == -1 {
		t.Fatalf("could not find path end in: %q", rest)
	}
	tmpPath := rest[:end]

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("temp file %q should exist: %v", tmpPath, err)
	}
	if string(data) != string(input) {
		t.Errorf("temp file content = %q, want %q", data, input)
	}
	os.Remove(tmpPath)
}
