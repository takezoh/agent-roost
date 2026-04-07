package claude

import (
	"strings"
	"testing"
)

func TestFormatTranscript_UserText(t *testing.T) {
	line := `{"type":"user","message":{"content":"hello world"}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "YOU>") || !strings.Contains(got, "hello world") {
		t.Errorf("got %q, want user prompt with YOU>", got)
	}
}

func TestFormatTranscript_UserTextBlocks(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"text","text":"prompt text"}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "prompt text") {
		t.Errorf("got %q, want prompt text", got)
	}
}

func TestFormatTranscript_UserToolResult(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"some output data"}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "16 chars") {
		t.Errorf("got %q, want chars count", got)
	}
}

func TestFormatTranscript_UserToolResultError(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":"fail","is_error":true}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "error") {
		t.Errorf("got %q, want error marker", got)
	}
}

func TestFormatTranscript_UserToolResultEmpty(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"tool_result","content":""}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "← ok") {
		t.Errorf("got %q, want ok", got)
	}
}

func TestFormatTranscript_AssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"response text"}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "response text") {
		t.Errorf("got %q, want response text", got)
	}
}

func TestFormatTranscript_AssistantToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"src/main.go"}}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "Read") || !strings.Contains(got, "src/main.go") {
		t.Errorf("got %q", got)
	}
}

func TestFormatTranscript_AssistantToolUseBash(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"make build"}}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "Bash") || !strings.Contains(got, "make build") {
		t.Errorf("got %q", got)
	}
}

func TestFormatTranscript_AssistantToolUseAgent(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Agent","input":{"description":"explore codebase"}}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "Agent") || !strings.Contains(got, "explore codebase") {
		t.Errorf("got %q", got)
	}
}

func TestFormatTranscript_AssistantToolUseUnknown(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"CustomTool","input":{"x":"y"}}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "CustomTool") {
		t.Errorf("got %q, want CustomTool", got)
	}
}

func TestFormatTranscript_AssistantMultipleBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"doing work"},{"type":"tool_use","name":"Read","input":{"file_path":"a.go"}},{"type":"tool_use","name":"Edit","input":{"file_path":"b.go"}}]}}`
	got := FormatTranscript(line)
	if !strings.Contains(got, "doing work") {
		t.Error("missing text block")
	}
	if strings.Count(got, "▸") != 2 {
		t.Errorf("expected 2 tool markers, got %d", strings.Count(got, "▸"))
	}
}

func TestFormatTranscript_AssistantThinking(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"thinking","text":"internal thought"}]}}`
	got := FormatTranscript(line)
	if got != "" {
		t.Errorf("thinking should produce empty output, got %q", got)
	}
}

func TestFormatTranscript_SkipsMetadata(t *testing.T) {
	types := []string{
		`{"type":"file-history-snapshot","messageId":"abc"}`,
		`{"type":"system","subtype":"local_command","content":""}`,
		`{"type":"queue-operation","operation":"enqueue"}`,
		`{"type":"custom-title","customTitle":"my-title"}`,
		`{"type":"agent-name","agentName":"test"}`,
		`{"type":"attachment","attachment":{}}`,
	}
	for _, line := range types {
		got := FormatTranscript(line)
		if got != "" {
			t.Errorf("FormatTranscript(%s) = %q, want empty", line, got)
		}
	}
}

func TestFormatTranscript_InvalidJSON(t *testing.T) {
	got := FormatTranscript("not json")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestFormatTranscript_EmptyUserContent(t *testing.T) {
	line := `{"type":"user","message":{"content":"  "}}`
	got := FormatTranscript(line)
	if got != "" {
		t.Errorf("got %q, want empty for whitespace-only", got)
	}
}

func TestFormatTranscript_UserSystemTag(t *testing.T) {
	line := `{"type":"user","message":{"content":"<local-command-caveat>some caveat</local-command-caveat>"}}`
	got := FormatTranscript(line)
	if got != "" {
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

func TestFormatTranscript_MultiLine(t *testing.T) {
	raw := `{"type":"user","message":{"content":"first"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"second"}]}}
{"type":"file-history-snapshot","messageId":"x"}`
	got := FormatTranscript(raw)
	if !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("got %q, want both entries", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2 (metadata skipped)", len(lines))
	}
}
