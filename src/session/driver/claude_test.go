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
{"type":"user","message":{"role":"user","content":"the last prompt"}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
	if meta.Title != "my-session-name" {
		t.Errorf("Title = %q, want %q", meta.Title, "my-session-name")
	}
	if meta.LastPrompt != "the last prompt" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "the last prompt")
	}
	if meta.Source != "abc.jsonl" {
		t.Errorf("Source = %q, want %q", meta.Source, "abc.jsonl")
	}
}

func TestClaude_ResolveMeta_WithSource(t *testing.T) {
	jsonl1 := `{"type":"custom-title","customTitle":"session-one"}
`
	jsonl2 := `{"type":"custom-title","customTitle":"session-two"}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/aaa.jsonl": &fstest.MapFile{
			Data: []byte(jsonl1),
		},
		".claude/projects/-workspace-myproject/bbb.jsonl": &fstest.MapFile{
			Data: []byte(jsonl2),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "aaa.jsonl")
	if meta.Title != "session-one" {
		t.Errorf("Title = %q, want %q", meta.Title, "session-one")
	}
	if meta.Source != "aaa.jsonl" {
		t.Errorf("Source = %q, want %q", meta.Source, "aaa.jsonl")
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
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
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
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}

func TestClaude_ResolveMeta_TaskCreateSubjects(t *testing.T) {
	jsonl := `{"type":"user","message":{"content":"hello"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Fix login bug"}},{"type":"tool_use","name":"Read","input":{"file_path":"main.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Add login tests"}}]}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
	if len(meta.Subjects) != 2 {
		t.Fatalf("Subjects len = %d, want 2", len(meta.Subjects))
	}
	if meta.Subjects[0] != "Fix login bug" {
		t.Errorf("Subjects[0] = %q, want %q", meta.Subjects[0], "Fix login bug")
	}
	if meta.Subjects[1] != "Add login tests" {
		t.Errorf("Subjects[1] = %q, want %q", meta.Subjects[1], "Add login tests")
	}
	if meta.LastPrompt != "hello" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "hello")
	}
}

func TestClaude_ResolveMeta_NoTaskCreate(t *testing.T) {
	jsonl := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"main.go"}}]}}
`
	fsys := fstest.MapFS{
		".claude/projects/-workspace-myproject/abc.jsonl": &fstest.MapFile{
			Data: []byte(jsonl),
		},
	}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
	if len(meta.Subjects) != 0 {
		t.Errorf("Subjects len = %d, want 0", len(meta.Subjects))
	}
}

func TestGeneric_ResolveMeta_Empty(t *testing.T) {
	fsys := fstest.MapFS{}
	d := NewGeneric("gemini")
	meta := d.ResolveMeta(fsys, "/workspace/myproject", "")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}
