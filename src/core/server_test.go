package core

import "testing"

func TestResolveCommandAlias(t *testing.T) {
	aliases := map[string]string{
		"clw": "claude --worktree",
		"cw":  "codex --workspace",
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"known alias", "clw", "claude --worktree"},
		{"trimmed alias", "  clw  ", "claude --worktree"},
		{"unknown passes through", "claude", "claude"},
		{"unknown with args passes through", "claude --worktree", "claude --worktree"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveCommandAlias(aliases, tc.in)
			if got != tc.want {
				t.Errorf("ResolveCommandAlias(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestResolveCommandAlias_NilMap(t *testing.T) {
	if got := ResolveCommandAlias(nil, "clw"); got != "clw" {
		t.Errorf("nil map should pass through, got %q", got)
	}
}
