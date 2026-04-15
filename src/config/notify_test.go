package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNotifyRuleMatches(t *testing.T) {
	home, _ := os.UserHomeDir()

	tests := []struct {
		name    string
		rule    NotifyRule
		driver  string
		command string
		project string
		kind    string
		want    bool
	}{
		// Single axis: driver exact match
		{
			name:   "driver exact match",
			rule:   NotifyRule{Driver: "claude"},
			driver: "claude", command: "claude", project: "/home/user/prjA", kind: "done",
			want: true,
		},
		{
			name:   "driver mismatch",
			rule:   NotifyRule{Driver: "claude"},
			driver: "codex", command: "codex", project: "/home/user/prjA", kind: "done",
			want: false,
		},
		// Single axis: command
		{
			name:   "command match",
			rule:   NotifyRule{Command: "npm"},
			driver: "generic", command: "npm", project: "/home/user/prjA", kind: "done",
			want: true,
		},
		// Single axis: project — full absolute path
		{
			name:   "project exact match",
			rule:   NotifyRule{Project: "/home/user/prjA"},
			driver: "claude", command: "claude", project: "/home/user/prjA", kind: "done",
			want: true,
		},
		{
			name:   "project wildcard under directory",
			rule:   NotifyRule{Project: "/home/user/*"},
			driver: "claude", command: "claude", project: "/home/user/prjA", kind: "done",
			want: true,
		},
		{
			name:   "project wildcard no match: different parent",
			rule:   NotifyRule{Project: "/home/user/prj*"},
			driver: "claude", command: "claude", project: "/home/other/prjA", kind: "done",
			want: false,
		},
		// Tilde expansion in pattern
		{
			name:    "tilde-expanded project pattern matches",
			rule:    NotifyRule{Project: "~/projects/*"},
			driver:  "claude",
			command: "claude",
			project: filepath.Join(home, "projects", "myrepo"),
			kind:    "done",
			want:    true,
		},
		{
			name:    "tilde-expanded project pattern no match",
			rule:    NotifyRule{Project: "~/projects/*"},
			driver:  "claude",
			command: "claude",
			project: filepath.Join(home, "work", "myrepo"),
			kind:    "done",
			want:    false,
		},
		// Single axis: kind
		{
			name:   "kind pending_approval",
			rule:   NotifyRule{Kind: "pending_approval"},
			driver: "claude", command: "claude", project: "/p", kind: "pending_approval",
			want: true,
		},
		{
			name:   "kind mismatch",
			rule:   NotifyRule{Kind: "pending_approval"},
			driver: "claude", command: "claude", project: "/p", kind: "done",
			want: false,
		},
		// AND: all axes must match
		{
			name:   "AND: driver+project+kind all match",
			rule:   NotifyRule{Driver: "claude", Project: "/home/user/prjA", Kind: "pending_approval"},
			driver: "claude", command: "claude", project: "/home/user/prjA", kind: "pending_approval",
			want: true,
		},
		{
			name:   "AND: driver matches but project doesn't",
			rule:   NotifyRule{Driver: "claude", Project: "/home/user/prjA", Kind: "pending_approval"},
			driver: "claude", command: "claude", project: "/home/user/prjB", kind: "pending_approval",
			want: false,
		},
		// Wildcard: empty string = any
		{
			name:   "all empty = match any",
			rule:   NotifyRule{},
			driver: "claude", command: "npm", project: "/some/path", kind: "done",
			want: true,
		},
		// Wildcard: "*" = any
		{
			name:   "explicit * = match any",
			rule:   NotifyRule{Driver: "*", Command: "*", Project: "*", Kind: "*"},
			driver: "claude", command: "npm", project: "/some/path", kind: "done",
			want: true,
		},
		// Glob patterns for driver/command
		{
			name:   "driver glob claude* matches claude-custom",
			rule:   NotifyRule{Driver: "claude*"},
			driver: "claude-custom", command: "claude-custom", project: "/p", kind: "done",
			want: true,
		},
		// Kind "*"
		{
			name:   "kind * matches pending_approval",
			rule:   NotifyRule{Kind: "*"},
			driver: "x", command: "x", project: "/p", kind: "pending_approval",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.Matches(tt.driver, tt.command, tt.project, tt.kind)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNotificationsConfigAnyMatch(t *testing.T) {
	empty := &NotificationsConfig{}
	if empty.AnyMatch("claude", "claude", "/p", "done") {
		t.Error("empty rules should never match")
	}

	cfg := &NotificationsConfig{
		Rules: []NotifyRule{
			{Driver: "claude", Kind: "pending_approval"},
			{Command: "npm", Kind: "done"},
		},
	}
	if !cfg.AnyMatch("claude", "claude", "/p", "pending_approval") {
		t.Error("expected first rule to match")
	}
	if !cfg.AnyMatch("generic", "npm", "/p", "done") {
		t.Error("expected second rule to match")
	}
	if cfg.AnyMatch("codex", "codex", "/p", "done") {
		t.Error("no rule should match codex+done")
	}
}

func TestNotificationsConfigValidate(t *testing.T) {
	valid := &NotificationsConfig{
		Rules: []NotifyRule{
			{Driver: "claude*", Project: "/home/user/prj*", Kind: "done"},
			{Command: "*", Kind: "pending_approval"},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("valid config validation failed: %v", err)
	}

	// Invalid glob pattern (path.Match syntax error)
	invalid := &NotificationsConfig{
		Rules: []NotifyRule{
			{Driver: "claude["},
		},
	}
	if err := invalid.Validate(); err == nil {
		t.Error("expected error for invalid glob pattern")
	}
}
