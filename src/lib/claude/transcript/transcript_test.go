package transcript

import (
	"strings"
	"testing"
)

func renderLine(line string) string {
	p := NewParser(ParserOptions{})
	return RenderEntries(p.ParseLines([]byte(line)))
}

func TestParser_UserText(t *testing.T) {
	line := `{"type":"user","message":{"content":"hello world"}}`
	got := renderLine(line)
	if !strings.Contains(got, "YOU>") || !strings.Contains(got, "hello world") {
		t.Errorf("got %q, want user prompt with YOU>", got)
	}
}

func TestParser_UserTextBlocks(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"text","text":"prompt text"}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "prompt text") {
		t.Errorf("got %q, want prompt text", got)
	}
}

func TestParser_UserToolResult(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"some output data"}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "16 chars") {
		t.Errorf("got %q, want chars count", got)
	}
}

func TestParser_UserToolResultError(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"fail","is_error":true}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "error") {
		t.Errorf("got %q, want error marker", got)
	}
}

func TestParser_UserToolResultEmpty(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":""}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "← ok") {
		t.Errorf("got %q, want ok", got)
	}
}

func TestParser_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"response text"}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "response text") {
		t.Errorf("got %q, want response text", got)
	}
}

func TestParser_AssistantToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"src/main.go"}}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "Read") || !strings.Contains(got, "src/main.go") {
		t.Errorf("got %q", got)
	}
}

func TestParser_AssistantToolUseBash(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"make build"}}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "Bash") || !strings.Contains(got, "make build") {
		t.Errorf("got %q", got)
	}
}

func TestParser_AssistantToolUseAgent(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","input":{"description":"explore codebase"}}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "Agent") || !strings.Contains(got, "explore codebase") {
		t.Errorf("got %q", got)
	}
}

func TestParser_AssistantToolUseUnknown(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"CustomTool","input":{"x":"y"}}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "CustomTool") {
		t.Errorf("got %q, want CustomTool", got)
	}
}

func TestParser_AssistantMultipleBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"doing work"},{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},{"type":"tool_use","name":"Edit","input":{"file_path":"b.go"}}]}}`
	got := renderLine(line)
	if !strings.Contains(got, "doing work") {
		t.Error("missing text block")
	}
	if strings.Count(got, "▸") != 2 {
		t.Errorf("expected 2 tool markers, got %d", strings.Count(got, "▸"))
	}
}

func TestParser_AssistantThinking(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","text":"internal thought"}]}}`
	got := renderLine(line)
	if got != "" {
		t.Errorf("thinking should produce empty output, got %q", got)
	}
}

func TestParser_SkipsSilentMetadata(t *testing.T) {
	// Types that should not produce any visible render line.
	types := []string{
		`{"type":"file-history-snapshot","messageId":"abc"}`,
		`{"type":"queue-operation","operation":"enqueue"}`,
		`{"type":"attachment","attachment":{}}`,
	}
	for _, line := range types {
		got := renderLine(line)
		if got != "" {
			t.Errorf("renderLine(%s) = %q, want empty", line, got)
		}
	}
}

func TestParser_InvalidJSON(t *testing.T) {
	if got := renderLine("not json"); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestParser_EmptyUserContent(t *testing.T) {
	line := `{"type":"user","message":{"content":"  "}}`
	if got := renderLine(line); got != "" {
		t.Errorf("got %q, want empty for whitespace-only", got)
	}
}

func TestParser_UserSystemTag(t *testing.T) {
	line := `{"type":"user","message":{"content":"<local-command-caveat>some caveat</local-command-caveat>"}}`
	if got := renderLine(line); got != "" {
		t.Errorf("system tag should be skipped, got %q", got)
	}
}

func TestTranscriptTruncate(t *testing.T) {
	short := "hello"
	if got := truncate(short, 10); got != "hello" {
		t.Errorf("truncate(%q, 10) = %q", short, got)
	}
	long := strings.Repeat("a", 100)
	got := truncate(long, 80)
	if len([]rune(got)) != 81 {
		t.Errorf("truncate len = %d, want 81", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("truncated string should end with …")
	}
}

func TestParser_MultiLine(t *testing.T) {
	raw := `{"type":"user","message":{"content":"first"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"second"}]}}
{"type":"file-history-snapshot","messageId":"x"}`
	got := renderLine(raw)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("got %q, want both entries", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2 (metadata skipped)", len(lines))
	}
}

func TestParser_ParserReusable(t *testing.T) {
	p := NewParser(ParserOptions{})
	_ = p.ParseLines([]byte(`{"type":"user","message":{"content":"first"}}`))
	second := p.ParseLines([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"second"}]}}`))
	if len(second) != 1 || second[0].Text != "second" {
		t.Errorf("reusable parser failed, got %+v", second)
	}
	p.Reset()
	third := p.ParseLines([]byte(`{"type":"user","message":{"content":"third"}}`))
	if len(third) != 1 || third[0].Text != "third" {
		t.Errorf("after Reset, got %+v", third)
	}
}
