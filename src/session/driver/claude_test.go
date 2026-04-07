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

func TestClaude_ResolveMeta(t *testing.T) {
	jsonl := `{"type":"user","message":{"role":"user","content":"first prompt"}}
{"type":"custom-title","customTitle":"my-session-name","sessionId":"abc"}
{"type":"user","message":{"role":"user","content":"最後のプロンプト"}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject")
	if meta.Title != "my-session-name" {
		t.Errorf("Title = %q, want %q", meta.Title, "my-session-name")
	}
	if meta.LastPrompt != "最後のプロンプト" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "最後のプロンプト")
	}
}

func TestClaude_ResolveMeta_NoCustomTitle(t *testing.T) {
	jsonl := `{"type":"user","message":{"role":"user","content":"fallback prompt"}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject")
	if meta.Title != "" {
		t.Errorf("Title = %q, want empty", meta.Title)
	}
	if meta.LastPrompt != "fallback prompt" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "fallback prompt")
	}
}

func TestClaude_ResolveMeta_NoFiles(t *testing.T) {
	fsys := fstest.MapFS{}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}

func TestGeneric_ResolveMeta_Empty(t *testing.T) {
	fsys := fstest.MapFS{}
	d := NewGeneric("gemini")
	meta := d.ResolveMeta(fsys, "/workspace/myproject")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}
