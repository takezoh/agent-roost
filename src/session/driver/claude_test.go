package driver

import (
	"testing"
	"testing/fstest"
)

func ctxWith(state map[string]string) SessionContext {
	return SessionContext{Command: "claude", DriverState: state}
}

func TestClaude_SpawnCommand(t *testing.T) {
	d := Claude{}
	if got := d.SpawnCommand("claude", ctxWith(nil)); got != "claude" {
		t.Errorf("empty session ID: got %q, want %q", got, "claude")
	}
	if got := d.SpawnCommand("claude", ctxWith(map[string]string{"session_id": "abc-123"})); got != "claude --resume abc-123" {
		t.Errorf("with session ID: got %q, want %q", got, "claude --resume abc-123")
	}
}

func TestGeneric_SpawnCommand(t *testing.T) {
	d := NewGeneric("gemini")
	if got := d.SpawnCommand("gemini", SessionContext{}); got != "gemini" {
		t.Errorf("empty session ID: got %q, want %q", got, "gemini")
	}
	// Generic ignores DriverState — no resume support.
	if got := d.SpawnCommand("gemini", SessionContext{DriverState: map[string]string{"session_id": "abc-123"}}); got != "gemini" {
		t.Errorf("with session ID: got %q, want %q", got, "gemini")
	}
}

func TestClaude_TranscriptFilePath(t *testing.T) {
	d := Claude{}
	tests := []struct {
		name string
		home string
		ctx  SessionContext
		want string
	}{
		{
			name: "plain project",
			home: "/home/u",
			ctx: SessionContext{
				DriverState: map[string]string{
					"session_id":  "abc",
					"working_dir": "/workspace/myproject",
				},
			},
			want: "/home/u/.claude/projects/-workspace-myproject/abc.jsonl",
		},
		{
			name: "worktree project",
			home: "/home/u",
			ctx: SessionContext{
				DriverState: map[string]string{
					"session_id":  "xyz",
					"working_dir": "/workspace/agent-roost/.claude/worktrees/foo",
				},
			},
			want: "/home/u/.claude/projects/-workspace-agent-roost--claude-worktrees-foo/xyz.jsonl",
		},
		{
			name: "agent-reported transcript wins",
			home: "/home/u",
			ctx: SessionContext{
				DriverState: map[string]string{
					"session_id":      "abc",
					"working_dir":     "/workspace/myproject",
					"transcript_path": "/explicit/path/abc.jsonl",
				},
			},
			want: "/explicit/path/abc.jsonl",
		},
		{
			name: "fallback to project when no working_dir",
			home: "/home/u",
			ctx: SessionContext{
				Project: "/workspace/myproject",
				DriverState: map[string]string{
					"session_id": "abc",
				},
			},
			want: "/home/u/.claude/projects/-workspace-myproject/abc.jsonl",
		},
		{name: "empty home", ctx: ctxWith(map[string]string{"session_id": "abc", "working_dir": "/x"})},
		{name: "empty session id", home: "/home/u", ctx: ctxWith(map[string]string{"working_dir": "/x"})},
		{name: "no working dir, no project", home: "/home/u", ctx: ctxWith(map[string]string{"session_id": "abc"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.TranscriptFilePath(tt.home, tt.ctx); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClaude_WorkingDir(t *testing.T) {
	d := Claude{}
	if got := d.WorkingDir(ctxWith(map[string]string{"working_dir": "/foo"})); got != "/foo" {
		t.Errorf("got %q, want /foo", got)
	}
	if got := d.WorkingDir(ctxWith(nil)); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestClaude_IdentityKey(t *testing.T) {
	if got := (Claude{}).IdentityKey(); got != "session_id" {
		t.Errorf("got %q, want session_id", got)
	}
}

func TestGeneric_TranscriptFilePath_Empty(t *testing.T) {
	d := NewGeneric("gemini")
	if got := d.TranscriptFilePath("/home/u", ctxWith(map[string]string{"session_id": "abc", "working_dir": "/workspace/x"})); got != "" {
		t.Errorf("expected empty path, got %q", got)
	}
}

func TestGeneric_IdentityKey_Empty(t *testing.T) {
	if got := NewGeneric("gemini").IdentityKey(); got != "" {
		t.Errorf("got %q, want empty", got)
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
{"type":"last-prompt","lastPrompt":"the last prompt","sessionId":"abc"}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-myproject/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		Project: "/workspace/myproject",
		DriverState: map[string]string{
			"session_id":  "abc",
			"working_dir": "/workspace/myproject",
		},
	})
	if meta.Title != "my-session-name" {
		t.Errorf("Title = %q, want %q", meta.Title, "my-session-name")
	}
	if meta.LastPrompt != "the last prompt" {
		t.Errorf("LastPrompt = %q, want %q", meta.LastPrompt, "the last prompt")
	}
}

func TestClaude_ResolveMeta_PrefersReportedPath(t *testing.T) {
	// Worktree-style transcript path: project recorded in roost is the
	// canonical repo, but Claude wrote the JSONL under the worktree directory.
	// Driver must read the transcript_path key as given (not derive it).
	jsonl := `{"type":"custom-title","customTitle":"worktree-session"}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-agent-roost--claude-worktrees-foo/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		Project: "/workspace/agent-roost",
		DriverState: map[string]string{
			"session_id":      "abc",
			"transcript_path": "/home/u/.claude/projects/-workspace-agent-roost--claude-worktrees-foo/abc.jsonl",
		},
	})
	if meta.Title != "worktree-session" {
		t.Errorf("Title = %q, want %q", meta.Title, "worktree-session")
	}
}

func TestClaude_ResolveMeta_EmptyState(t *testing.T) {
	d := Claude{}
	meta := d.ResolveMeta(fstest.MapFS{}, "/home/u", SessionContext{})
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta for empty state, got %+v", meta)
	}
}

func TestClaude_ResolveMeta_NoFile(t *testing.T) {
	fsys := fstest.MapFS{}
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		DriverState: map[string]string{
			"session_id":      "missing",
			"transcript_path": "/home/u/.claude/projects/-x/missing.jsonl",
		},
	})
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta when file is missing, got %+v", meta)
	}
}

func TestClaude_ResolveMeta_TaskCreateSubjects(t *testing.T) {
	jsonl := `{"type":"user","message":{"content":"hello"}}
{"type":"last-prompt","lastPrompt":"hello","sessionId":"abc"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Fix login bug"}},{"type":"tool_use","name":"Read","input":{"file_path":"main.go"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"TaskCreate","input":{"subject":"Add login tests"}}]}}
`
	fsys := mapFSWithTranscript("home/u/.claude/projects/-workspace-myproject/abc.jsonl", jsonl)
	d := Claude{}
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		Project: "/workspace/myproject",
		DriverState: map[string]string{
			"session_id":  "abc",
			"working_dir": "/workspace/myproject",
		},
	})
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
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		Project: "/workspace/myproject",
		DriverState: map[string]string{
			"session_id":  "abc",
			"working_dir": "/workspace/myproject",
		},
	})
	if len(meta.Subjects) != 0 {
		t.Errorf("Subjects len = %d, want 0", len(meta.Subjects))
	}
}

func TestGeneric_ResolveMeta_Empty(t *testing.T) {
	fsys := fstest.MapFS{}
	d := NewGeneric("gemini")
	meta := d.ResolveMeta(fsys, "/home/u", SessionContext{
		DriverState: map[string]string{
			"transcript_path": "/home/u/.claude/projects/x/abc.jsonl",
		},
	})
	if meta.Title != "" || meta.LastPrompt != "" {
		t.Errorf("expected empty meta, got Title=%q LastPrompt=%q", meta.Title, meta.LastPrompt)
	}
}
