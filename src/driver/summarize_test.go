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

func TestStripHookLinesRemovesGeminiHookTrailer(t *testing.T) {
	in := "actual summary line one\nline two\n" +
		"Created execution plan for SessionEnd: 1 hook(s) to execute in parallel\n" +
		"Expanding hook command: /path/to/roost event gemini (cwd: /tmp)\n" +
		"Hook execution for SessionEnd: 1 hooks executed successfully, total duration: 28ms\n"
	got := stripHookLines(in)
	want := "actual summary line one\nline two"
	if strings.TrimSpace(got) != want {
		t.Errorf("stripHookLines failed\n got: %q\nwant: %q", got, want)
	}
}

func TestStripHookLinesPreservesNonMatchingTail(t *testing.T) {
	in := "real summary\nstill summary text"
	if got := stripHookLines(in); got != in {
		t.Errorf("stripHookLines mutated non-hook text\n got: %q\nwant: %q", got, in)
	}
}

func TestStripHookLinesStopsAtFirstNonHookLine(t *testing.T) {
	// "Hook execution for X" is the trailing match, but the line above it
	// is real content — must NOT be stripped.
	in := "summary\nimportant trailing detail\n" +
		"Hook execution for SessionEnd: 1 hooks executed successfully\n"
	got := stripHookLines(in)
	want := "summary\nimportant trailing detail"
	if strings.TrimSpace(got) != want {
		t.Errorf("expected stop at non-hook line\n got: %q\nwant: %q", got, want)
	}
}

func TestStripHookLinesHandlesNoTrailer(t *testing.T) {
	in := "just a summary"
	if got := stripHookLines(in); got != in {
		t.Errorf("expected no-op, got %q", got)
	}
}

func TestSummarizeWithCommandStripsTrailingHookLog(t *testing.T) {
	// Pass payload via stdin (cat reads it back) so actual newlines are preserved.
	payload := "the real summary\nHook execution for SessionEnd: 1 hooks executed successfully"
	got, err := summarizeWithCommand(context.Background(), payload, "cat")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "the real summary" {
		t.Errorf("expected hook log stripped, got %q", got)
	}
}
