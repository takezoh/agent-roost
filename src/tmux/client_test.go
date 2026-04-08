package tmux

import "testing"

func TestParseRoostWindows(t *testing.T) {
	// 10-field tab format: window_id, @roost_id, project, command, created_at,
	// tags, agent_pane, agent_session, agent_workdir, agent_transcript
	out := "@1\tabc123\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\t\t\t\n" +
		"@2\t\t\t\t\t\t\t\t\t\n" + // skipped: empty @roost_id
		"@3\tdef456\t/tmp/other\tclaude --worktree\t2026-04-08T12:01:00Z\t[{\"text\":\"main\"}]\t%5\tagent-789\t/tmp/other/.claude/worktrees/foo\t/home/u/.claude/projects/-tmp-other--claude-worktrees-foo/agent-789.jsonl\n"

	windows := parseRoostWindows(out)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(windows), windows)
	}

	w := windows[0]
	if w.WindowID != "@1" || w.ID != "abc123" || w.Project != "/tmp/proj" ||
		w.Command != "claude" || w.CreatedAt != "2026-04-08T12:00:00Z" ||
		w.Tags != "[]" || w.AgentPaneID != "" || w.AgentSessionID != "" ||
		w.AgentWorkingDir != "" || w.AgentTranscriptPath != "" {
		t.Fatalf("window 0 mismatch: %+v", w)
	}

	w = windows[1]
	if w.WindowID != "@3" || w.ID != "def456" || w.AgentPaneID != "%5" ||
		w.AgentSessionID != "agent-789" ||
		w.Tags != `[{"text":"main"}]` ||
		w.AgentWorkingDir != "/tmp/other/.claude/worktrees/foo" ||
		w.AgentTranscriptPath != "/home/u/.claude/projects/-tmp-other--claude-worktrees-foo/agent-789.jsonl" {
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
	// Pre-AgentPaneID 9-field format should now be skipped.
	if got := parseRoostWindows("@1\tabc\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\t\t\n"); len(got) != 0 {
		t.Fatalf("expected 0 windows for legacy 9-field line, got %d", len(got))
	}
}
