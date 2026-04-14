package main

import "testing"

func TestResolveShellDisplayFromValues(t *testing.T) {
	cases := []struct {
		tmuxDefault string
		envSHELL    string
		want        string
	}{
		{"/usr/bin/zsh", "/bin/bash", "zsh"},
		{"", "/bin/bash", "bash"},
		{"", "/usr/bin/zsh", "zsh"},
		{"", "", "shell"},
		{".", "", "shell"},
		{"", ".", "shell"},
		{".", ".", "shell"},
	}
	for _, c := range cases {
		got := resolveShellDisplayFromValues(c.tmuxDefault, c.envSHELL)
		if got != c.want {
			t.Errorf("resolveShellDisplayFromValues(%q, %q) = %q, want %q",
				c.tmuxDefault, c.envSHELL, got, c.want)
		}
	}
}
