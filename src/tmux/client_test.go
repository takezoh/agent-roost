package tmux

import "testing"

func TestParseRoostWindows(t *testing.T) {
	// 7-field tab format: window_id, @roost_id, project, command, created_at,
	// tags, @roost_driver_state (JSON-encoded map[string]string).
	driverState := `{"session_id":"agent-789","working_dir":"/tmp/other/.claude/worktrees/foo","transcript_path":"/home/u/.claude/projects/-tmp-other--claude-worktrees-foo/agent-789.jsonl"}`
	out := "@1\tabc123\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\n" +
		"@2\t\t\t\t\t\t\n" + // skipped: empty @roost_id
		"@3\tdef456\t/tmp/other\tclaude --worktree\t2026-04-08T12:01:00Z\t[{\"text\":\"main\"}]\t" + driverState + "\n"

	windows := parseRoostWindows(out)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(windows), windows)
	}

	w := windows[0]
	if w.WindowID != "@1" || w.ID != "abc123" || w.Project != "/tmp/proj" ||
		w.Command != "claude" || w.CreatedAt != "2026-04-08T12:00:00Z" ||
		w.Tags != "[]" || w.DriverState != "" {
		t.Fatalf("window 0 mismatch: %+v", w)
	}

	w = windows[1]
	if w.WindowID != "@3" || w.ID != "def456" ||
		w.Tags != `[{"text":"main"}]` ||
		w.DriverState != driverState {
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
	// Pre-DriverState 6-field format should now be skipped.
	if got := parseRoostWindows("@1\tabc\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\n"); len(got) != 0 {
		t.Fatalf("expected 0 windows for legacy 6-field line, got %d", len(got))
	}
}
