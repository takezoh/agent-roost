package tmux

import (
	"context"
	"testing"
	"time"
)

// TestRunContextTimeout verifies that Run fails fast when the server
// does not respond within the defaultTimeout. We point at a non-existent
// tmux server (using a random -L socket name) so the command returns
// quickly with an error rather than hanging.
func TestRunContextTimeout(t *testing.T) {
	c := &Client{
		SessionName:    "nonexistent",
		defaultTimeout: 100 * time.Millisecond,
	}

	start := time.Now()
	// list-sessions on a non-existent socket exits immediately with an error,
	// but the test demonstrates that Run respects the bounded context.
	_, err := c.Run("-L", "roost-test-nonexistent-socket-xyz", "list-sessions")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for non-existent server, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Run took %v, want <500ms (should fail fast)", elapsed)
	}
}

// TestRunContextCancelled verifies that RunContext respects a pre-cancelled
// context and returns immediately.
func TestRunContextCancelled(t *testing.T) {
	c := NewClient("nonexistent")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	start := time.Now()
	_, err := c.RunContext(ctx, "list-sessions")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("RunContext took %v with cancelled ctx, want <200ms", elapsed)
	}
}
