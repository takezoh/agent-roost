package cli

import "testing"

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		sessionID string
		want      string
	}{
		{"empty session id returns base unchanged", "claude", "", "claude"},
		{"non-empty appends --resume flag", "claude", "abc-123", "claude --resume abc-123"},
		{"empty base + empty id stays empty", "", "", ""},
		{"empty base + id still appends", "", "abc", " --resume abc"},
		{"strips --worktree on resume", "claude --worktree", "abc", "claude --resume abc"},
		{"strips --worktree NAME on resume", "claude --worktree foo", "abc", "claude --resume abc"},
		{"strips --worktree=NAME on resume", "claude --worktree=foo", "abc", "claude --resume abc"},
		{"keeps subsequent flag after --worktree", "claude --worktree --verbose", "abc", "claude --verbose --resume abc"},
		{"keeps --worktree when not resuming", "claude --worktree", "", "claude --worktree"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResumeCommand(tt.base, tt.sessionID); got != tt.want {
				t.Errorf("ResumeCommand(%q, %q) = %q, want %q", tt.base, tt.sessionID, got, tt.want)
			}
		})
	}
}

func TestStripWorktreeFlag(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no flag", "claude", "claude"},
		{"bare --worktree", "claude --worktree", "claude"},
		{"--worktree with name", "claude --worktree foo", "claude"},
		{"--worktree=name", "claude --worktree=foo", "claude"},
		{"--worktree followed by flag (preserve flag)", "claude --worktree --verbose", "claude --verbose"},
		{"--worktree in the middle with name", "claude --verbose --worktree foo --resume bar", "claude --verbose --resume bar"},
		{"empty input", "", ""},
		{"only --worktree", "--worktree", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripWorktreeFlag(tt.in); got != tt.want {
				t.Errorf("StripWorktreeFlag(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
