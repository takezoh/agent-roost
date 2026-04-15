package plastic

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBranchFromWorkspaceInfo(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "root branch",
			input: "BR /main SocialVR@thirdverse@cloud\n",
			want:  "/main",
		},
		{
			name:  "nested branch",
			input: "BR /main/feature-x repo@server:8084\n",
			want:  "/main/feature-x",
		},
		{
			name:  "deep branch path",
			input: "BR /main/release/v2.0/hotfix repo@server:8084\n",
			want:  "/main/release/v2.0/hotfix",
		},
		{
			name:  "CRLF line ending",
			input: "BR /main SocialVR@thirdverse@cloud\r\n",
			want:  "/main",
		},
		{
			name:  "no trailing newline",
			input: "BR /main SocialVR@thirdverse@cloud",
			want:  "/main",
		},
		{
			name:  "branch only (no repo field)",
			input: "BR /main",
			want:  "/main",
		},
		{
			name:  "label selector (not a branch)",
			input: "LB my-label repo@server\n",
			want:  "",
		},
		{
			name:  "changeset selector",
			input: "CS 12345 repo@server\n",
			want:  "",
		},
		{
			name:  "empty output",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseBranchFromWorkspaceInfo(tt.input)
			if got != tt.want {
				t.Errorf("ParseBranchFromWorkspaceInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindPlasticRoot(t *testing.T) {
	// Build a temp tree: root/.plastic  root/sub/subsub
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".plastic"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	subsub := filepath.Join(sub, "subsub")
	if err := os.MkdirAll(subsub, 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		dir  string
		want string // "" means not found
	}{
		{"workspace root itself", root, root},
		{"direct child", sub, root},
		{"grandchild", subsub, root},
		{"outside workspace", t.TempDir(), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findPlasticRoot(tt.dir)
			if got != tt.want {
				t.Errorf("findPlasticRoot(%q) = %q, want %q", tt.dir, got, tt.want)
			}
		})
	}
}
