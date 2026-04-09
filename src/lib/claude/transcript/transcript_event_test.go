package transcript

import (
	"strings"
	"testing"
)

func TestParser_ThinkingHiddenByDefault(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"why is this happening"}]}}`))
	if len(entries) != 0 {
		t.Errorf("expected no entries when ShowThinking=false, got %+v", entries)
	}
}

func TestParser_ThinkingShown(t *testing.T) {
	p := NewParser(ParserOptions{ShowThinking: true})
	entries := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"why is this happening"}]}}`))
	if len(entries) != 1 || entries[0].Kind != KindAssistantThinking {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].Text != "why is this happening" {
		t.Errorf("Text = %q", entries[0].Text)
	}
	out := RenderEntries(entries)
	if !strings.Contains(out, "why is this happening") {
		t.Errorf("render missing text: %q", out)
	}
}

func TestParser_ThinkingLegacyTextField(t *testing.T) {
	// Older logs stored thinking under "text" rather than "thinking".
	p := NewParser(ParserOptions{ShowThinking: true})
	entries := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"thinking","text":"legacy thought"}]}}`))
	if len(entries) != 1 || entries[0].Text != "legacy thought" {
		t.Errorf("got %+v", entries)
	}
}

func TestRenderThinking_Truncates(t *testing.T) {
	lines := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		lines = append(lines, "line")
	}
	got := renderThinking(strings.Join(lines, "\n"))
	if !strings.Contains(got, "[+4 more lines]") {
		t.Errorf("expected truncation marker, got %q", got)
	}
}

func TestParser_System(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"system","subtype":"local_command","level":"info","content":"<local-command-stdout>ok</local-command-stdout>"}`))
	if len(entries) != 1 || entries[0].Kind != KindSystem {
		t.Fatalf("got %+v", entries)
	}
	if !strings.Contains(entries[0].Text, "local_command") {
		t.Errorf("Text = %q", entries[0].Text)
	}
	if !strings.Contains(RenderEntries(entries), "local_command") {
		t.Errorf("render missing subtype")
	}
}

func TestParser_SystemWithLevel(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"system","subtype":"warning","level":"warning","content":"slow"}`))
	if !strings.Contains(entries[0].Text, "warning:warning") {
		t.Errorf("Text = %q", entries[0].Text)
	}
}

func TestParser_Attachment(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"attachment","attachment":{"type":"deferred_tools_delta","addedNames":["TaskCreate","TaskUpdate"],"removedNames":[]}}`))
	if len(entries) != 1 || entries[0].Kind != KindAttachment {
		t.Fatalf("got %+v", entries)
	}
	out := RenderEntries(entries)
	if !strings.Contains(out, "TaskCreate") || !strings.Contains(out, "deferred_tools_delta") {
		t.Errorf("render = %q", out)
	}
}

func TestParser_AttachmentRemoved(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"attachment","attachment":{"type":"x","addedNames":[],"removedNames":["A","B","C","D","E"]}}`))
	if !strings.Contains(entries[0].Text, "+2 more") {
		t.Errorf("expected truncation, Text = %q", entries[0].Text)
	}
}

func TestParser_AttachmentEmpty(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"attachment","attachment":{"type":"x","addedNames":[],"removedNames":[]}}`))
	if len(entries) != 0 {
		t.Errorf("empty attachment should produce no entries, got %+v", entries)
	}
}

func TestParser_FileSnapshot(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"file-history-snapshot","snapshot":{"trackedFileBackups":[{"backupFileName":"a"},{"backupFileName":"b"}]}}`))
	if len(entries) != 1 || entries[0].Kind != KindFileSnapshot {
		t.Fatalf("got %+v", entries)
	}
	if !strings.Contains(entries[0].Text, "2 tracked") {
		t.Errorf("Text = %q", entries[0].Text)
	}
	// Render is suppressed by default.
	if RenderEntries(entries) != "" {
		t.Errorf("file-history-snapshot should render empty, got %q", RenderEntries(entries))
	}
}

func TestParser_CustomTitle(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"custom-title","customTitle":"my-session"}`))
	if len(entries) != 1 || entries[0].Kind != KindCustomTitle {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].Text != "my-session" {
		t.Errorf("Text = %q", entries[0].Text)
	}
	if !strings.Contains(RenderEntries(entries), "my-session") {
		t.Errorf("render missing title")
	}
}

func TestParser_AgentName(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"agent-name","agentName":"transcript-status"}`))
	if len(entries) != 1 || entries[0].Kind != KindAgentName {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].Text != "transcript-status" {
		t.Errorf("Text = %q", entries[0].Text)
	}
}

func TestParser_LastPrompt(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"last-prompt","lastPrompt":"hello rewind","sessionId":"abc"}`))
	if len(entries) != 1 || entries[0].Kind != KindLastPrompt {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].Text != "hello rewind" {
		t.Errorf("Text = %q", entries[0].Text)
	}
}

func TestParser_LastPromptEmpty(t *testing.T) {
	p := NewParser(ParserOptions{})
	entries := p.ParseLines([]byte(`{"type":"last-prompt","lastPrompt":""}`))
	if len(entries) != 1 || entries[0].Kind != KindLastPrompt {
		t.Fatalf("got %+v", entries)
	}
	if entries[0].Text != "" {
		t.Errorf("Text = %q, want empty", entries[0].Text)
	}
}
