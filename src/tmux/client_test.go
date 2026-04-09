package tmux

import "testing"

func TestParseRoostWindows(t *testing.T) {
	// 8-field tab format: window_id, @roost_id, project, command, created_at,
	// tags, @roost_agent_pane, @roost_driver_state.
	// Dynamic status (@roost_state*) is read separately via ListWindowOptions
	// — it belongs to state.Store, not Manager metadata.
	driverState := `{"session_id":"agent-789","working_dir":"/tmp/other/.claude/worktrees/foo","transcript_path":"/home/u/.claude/projects/-tmp-other--claude-worktrees-foo/agent-789.jsonl"}`
	out := "@1\tabc123\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t[]\t\t\n" +
		"@2\t\t\t\t\t\t\t\n" + // skipped: empty @roost_id
		"@3\tdef456\t/tmp/other\tclaude --worktree\t2026-04-08T12:01:00Z\t[{\"text\":\"main\"}]\t%5\t" + driverState + "\n"

	windows := parseRoostWindows(out)
	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d: %+v", len(windows), windows)
	}

	w := windows[0]
	if w.WindowID != "@1" || w.ID != "abc123" || w.Project != "/tmp/proj" ||
		w.Command != "claude" || w.CreatedAt != "2026-04-08T12:00:00Z" ||
		w.Tags != "[]" || w.AgentPaneID != "" || w.DriverState != "" {
		t.Fatalf("window 0 mismatch: %+v", w)
	}

	w = windows[1]
	if w.WindowID != "@3" || w.ID != "def456" || w.AgentPaneID != "%5" ||
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
	// 3 fields can't be padded into a valid roost window because @roost_id
	// is field 1, but project / command / created_at would all be empty,
	// which is fine — the parser only requires @roost_id to be non-empty.
	// We should accept this and just have empty trailing fields.
	got := parseRoostWindows("@1\tabc\tonly-three")
	if len(got) != 1 {
		t.Fatalf("expected 1 window (padded), got %d", len(got))
	}
	if got[0].ID != "abc" || got[0].Project != "only-three" {
		t.Fatalf("unexpected fields: %+v", got[0])
	}
}

// Regression: Client.Run trims trailing whitespace from the tmux output,
// which strips empty trailing fields. parseRoostWindows must right-pad
// the parts slice so a session whose driver_state is empty (the common
// case for a freshly created session) still parses to 8 fields. Without
// the pad, ReconcileWindows would treat the session as missing on the
// next polling tick and evict it from the Manager cache.
func TestParseRoostWindows_TrailingEmptyDriverState(t *testing.T) {
	// 8 fields where the trailing @roost_driver_state is empty.
	// The trailing tab gets trimmed by Run before parseRoostWindows sees it,
	// so the input here only has 7 actual values + 6 separators.
	out := "@1\tabc\t/tmp/proj\tclaude\t2026-04-08T12:00:00Z\t\t%5"
	windows := parseRoostWindows(out)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window despite trailing empty field, got %d", len(windows))
	}
	w := windows[0]
	if w.WindowID != "@1" || w.ID != "abc" || w.Project != "/tmp/proj" ||
		w.Command != "claude" || w.AgentPaneID != "%5" || w.DriverState != "" {
		t.Fatalf("unexpected fields after pad: %+v", w)
	}
}
