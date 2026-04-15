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

func TestFilteredRoostEnvStripsRoostSessionID(t *testing.T) {
	src := []string{
		"PATH=/usr/bin",
		"ROOST_SESSION_ID=should-be-dropped",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
	}
	out := filteredRoostEnv(src)

	for _, kv := range out {
		if kv == "ROOST_SESSION_ID=should-be-dropped" {
			t.Errorf("ROOST_SESSION_ID leaked into filtered env: %v", out)
		}
	}

	mustKeep := []string{
		"PATH=/usr/bin",
		"HOME=/home/take",
		"ANTHROPIC_API_KEY=keep-me",
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

func TestFilteredRoostEnvHandlesMalformedEntries(t *testing.T) {
	src := []string{"PATH=/usr/bin", "MALFORMED_NO_EQUALS"}
	out := filteredRoostEnv(src)
	if len(out) != 2 {
		t.Errorf("expected both entries preserved, got %v", out)
	}
}
