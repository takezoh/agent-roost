package driver

import (
	"testing"
	"testing/fstest"
)

func TestClaude_SpawnCommand(t *testing.T) {
	d := Claude{}
	if got := d.SpawnCommand("claude", ""); got != "claude" {
		t.Errorf("empty session ID: got %q, want %q", got, "claude")
	}
	if got := d.SpawnCommand("claude", "abc-123"); got != "claude --resume abc-123" {
		t.Errorf("with session ID: got %q, want %q", got, "claude --resume abc-123")
	}
}

func TestGeneric_SpawnCommand(t *testing.T) {
	d := NewGeneric("gemini")
	if got := d.SpawnCommand("gemini", ""); got != "gemini" {
		t.Errorf("empty session ID: got %q, want %q", got, "gemini")
	}
	// Generic ignores agentSessionID — no resume support.
	if got := d.SpawnCommand("gemini", "abc-123"); got != "gemini" {
		t.Errorf("with session ID: got %q, want %q", got, "gemini")
	}
}

// fstest.MapFS keys are unrooted; ResolveMeta strips the leading "/" before
// calling fs.Open, so absolute hook-event paths map directly onto these keys.
func mapFSWithTranscript(path, jsonl string) fstest.MapFS {
	return fstest.MapFS{
		path: &fstest.MapFile{Data: []byte(jsonl)},
	}
}

func TestClaude_ResolveMeta(t *testing.T) {
	jsonl := `{"type":"user","message":{"role":"user","content":"first prompt"}}
{"type":"custom-title","customTitle":"my-session-name","sessionId":"abc"}
{"type":"user","message":{"role":"user","content":"the last prompt"}}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-myproject/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/-workspace-myproject/abc.jsonl")
	if meta.Title != "my-session-name" {
		t.Errorf("Title = %q, want %q", meta.Title, "my-session-name")
	}
	if meta.LastPrompt != "the last prompt" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "the last prompt")
	}
}

func TestClaude_ResolveMeta_WorktreePath(t *testing.T) {
	// Worktree-style transcript path: project recorded in roost is the
	// canonical repo, but Claude wrote the JSONL under the worktree directory.
	// Driver must read the path as given (not derive it from project).
	jsonl := `{"type":"custom-title","customTitle":"worktree-session"}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-agent-roost--claude-worktrees-foo/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/-workspace-agent-roost--claude-worktrees-foo/abc.jsonl")
	if meta.Title != "worktree-session" {
		t.Errorf("Title = %q, want %q", meta.Title, "worktree-session")
	}
}

func TestClaude_ResolveMeta_EmptyPath(t *testing.T) {
	d := Claude{}
	meta := d.ResolveMeta(fstest.MapFS{}, "")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta for empty transcriptPath, got %+v", meta)
	}
}

func TestClaude_ResolveMeta_NoFile(t *testing.T) {
	fsys := fstest.MapFS{}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/-x/missing.jsonl")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta when file is missing, got %+v", meta)
	}
}

func TestClaude_ResolveMeta_TaskCreateSubjects(t *testing.T) {
	jsonl := `{"type":"user","message":{"content":"hello"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Fix login bug"}},{"type":"tool_use","name":"Read","input":{"file_path":"main.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Add login tests"}}]}}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-myproject/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/-workspace-myproject/abc.jsonl")
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
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-myproject/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/-workspace-myproject/abc.jsonl")
	if len(meta.Subjects) != 0 {
		t.Errorf("Subjects len = %d, want 0", len(meta.Subjects))
	}
}

func TestGeneric_ResolveMeta_Empty(t *testing.T) {
	fsys := fstest.MapFS{}
	d := NewGeneric("gemini")
	meta := d.ResolveMeta(fsys, "/home/u/.claude/projects/x/abc.jsonl")
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}
