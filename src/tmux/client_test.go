package tmux

import "testing"

func TestParseRoostWindows(t *testing.T) {
	out := "@1\tabc123\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\t\n" +
		"@2\t\t\t\t\t\t\t\n" + // skipped: empty @roost_id
		"@3\tdef456\t/tmp/other\tclaude\t2026-04-08T12:01:00Z\t[{\"text\":\"main\"}]\tagent-789\t/home/u/.claude/projects/-tmp-other/agent-789.jsonl\n"

	windows := parseRoostWindows(out)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(windows), windows)
	}

	w := windows[0]
	if w.WindowID != "@1" || w.ID != "abc123" || w.Project != "/tmp/proj" ||
		w.Command != "claude" || w.CreatedAt != "2026-04-08T12:00:00Z" ||
		w.Tags != "[]" || w.AgentSessionID != "" || w.AgentTranscriptPath != "" {
		t.Fatalf("window 0 mismatch: %+v", w)
	}

	w = windows[1]
	if w.WindowID != "@3" || w.ID != "def456" || w.AgentSessionID != "agent-789" ||
		w.Tags != `[{"text":"main"}]` ||
		w.AgentTranscriptPath != "/home/u/.claude/projects/-tmp-other/agent-789.jsonl" {
		t.Fatalf("window 1 mismatch: %+v", w)
	}
}

func TestParseRoostWindows_Empty(t *testing.T) {
	if got := parseRoostWindows(""); len(got) != 0 {
		t.Fatalf("expected 0 windows, got %d", len(got))
	}
}

func TestParseRoostWindows_TooFewFields(t *testing.T) {
	if got := parseRoostWindows("@1\tabc\tonly-three"); len(got) != 0 {
		t.Fatalf("expected 0 windows for malformed line, got %d", len(got))
	}
	// Old 7-field format (pre @roost_agent_transcript_path) should now be skipped.
	if got := parseRoostWindows("@1\tabc\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\n"); len(got) != 0 {
		t.Fatalf("expected 0 windows for legacy 7-field line, got %d", len(got))
	}
}
