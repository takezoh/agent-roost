package driver

import (
	"context"
	"strings"
	"testing"
)

func TestSummarizeWithCommand(t *testing.T) {
	ctx := context.Background()

	got, err := summarizeWithCommand(ctx, "hello world", "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", got)
	}
}

func TestSummarizeWithCommandTrimsOutput(t *testing.T) {
	ctx := context.Background()

	got, err := summarizeWithCommand(ctx, "x", "echo '  trimmed  '")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(got) != got {
		t.Errorf("output not trimmed: %q", got)
	}
}

func TestSummarizeWithCommandError(t *testing.T) {
	ctx := context.Background()

	_, err := summarizeWithCommand(ctx, "x", "false")
	if err == nil {
		t.Fatal("expected error from failing command, got nil")
	}
}

func TestFilteredRoostEnvStripsRoostPrefix(t *testing.T) {
	src := []string{
		"PATH=/usr/bin",
		"ROOST_SESSION_ID=drop",
		"ROOST_FRAME_ID=drop",
		"ROOST_W_PANE=drop",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
		"ROOSTERS=keep", // prefix ROOST but not ROOST_
	}
	out := filteredRoostEnv(src)

	for _, kv := range out {
		if strings.HasPrefix(kv, "ROOST_") {
			t.Errorf("ROOST_* leaked into filtered env: %q (full: %v)", kv, out)
		}
	}

	mustKeep := []string{
		"PATH=/usr/bin",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
		"ROOSTERS=keep",
	}
	for _, want := range mustKeep {
		found := false
		for _, kv := range out {
			if kv == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to survive filtering, got %v", want, out)
		}
	}
}

func TestSummarizeWithCommandDropsRoostFrameID(t *testing.T) {
	t.Setenv("ROOST_FRAME_ID", "leak")
	ctx := context.Background()
	// If ROOST_FRAME_ID were passed through, echo would print "leak".
	// filteredRoostEnv must strip it so the output is empty.
	got, err := summarizeWithCommand(ctx, "", `echo "$ROOST_FRAME_ID"`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("ROOST_FRAME_ID leaked into subprocess: got %q, want empty", got)
	}
}

func TestFilteredRoostEnvHandlesMalformedEntries(t *testing.T) {
	src := []string{"PATH=/usr/bin", "MALFORMED_NO_EQUALS"}
	out := filteredRoostEnv(src)
	if len(out) != 2 {
		t.Errorf("expected both entries preserved, got %v", out)
	}
}
