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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResumeCommand(tt.base, tt.sessionID); got != tt.want {
				t.Errorf("ResumeCommand(%q, %q) = %q, want %q", tt.base, tt.sessionID, got, tt.want)
			}
		})
	}
}
