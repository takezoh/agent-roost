package driver

import "testing"

func TestKind(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "claude", "claude"},
		{"with flag", "claude --worktree", "claude"},
		{"single env", "FOO=bar claude --worktree", "claude"},
		{"multiple env", "FOO=bar BAZ=qux claude", "claude"},
		{"absolute path", "/usr/local/bin/claude --resume abc", "claude"},
		{"quoted arg", `claude --system-prompt "be terse"`, "claude"},
		{"env only", "FOO=bar", ""},
		{"bash", "bash -lc 'foo'", "bash"},
		{"gemini", "gemini chat", "gemini"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Kind(tc.in)
			if got != tc.want {
				t.Errorf("Kind(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
