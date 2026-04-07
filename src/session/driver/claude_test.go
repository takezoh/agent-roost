package driver

import (
	"testing"
	"testing/fstest"
)

func TestClaudeProjectDir(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/take", "-home-take"},
		{"/home/take/.claude", "-home-take--claude"},
		{"/workspace/agent-roost", "-workspace-agent-roost"},
		{"/home/take/.dotfiles/config/nvim", "-home-take--dotfiles-config-nvim"},
	}
	for _, tt := range tests {
		got := ProjectDir(tt.path)
		if got != tt.want {
			t.Errorf("ProjectDir(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestClaude_ResolveTitle(t *testing.T) {
	jsonl := `{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"last-prompt","lastPrompt":"最後のプロンプト","sessionId":"abc"}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	got := d.ResolveTitle(fsys, "/workspace/myproject")
	if got != "最後のプロンプト" {
		t.Errorf("ResolveTitle() = %q, want %q", got, "最後のプロンプト")
	}
}

func TestClaude_ResolveTitle_NoFiles(t *testing.T) {
	fsys := fstest.MapFS{}
	d := Claude{}
	got := d.ResolveTitle(fsys, "/workspace/myproject")
	if got != "" {
		t.Errorf("ResolveTitle() = %q, want empty", got)
	}
}

func TestClaude_ResolveTitle_NoLastPrompt(t *testing.T) {
	jsonl := `{"type":"user","message":{"role":"user","content":"hello"}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	got := d.ResolveTitle(fsys, "/workspace/myproject")
	if got != "" {
		t.Errorf("ResolveTitle() = %q, want empty", got)
	}
}

func TestGeneric_ResolveTitle_Empty(t *testing.T) {
	fsys := fstest.MapFS{}
	d := NewGeneric("gemini")
	got := d.ResolveTitle(fsys, "/workspace/myproject")
	if got != "" {
		t.Errorf("ResolveTitle() = %q, want empty", got)
	}
}
