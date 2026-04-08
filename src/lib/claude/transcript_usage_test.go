package claude

import "testing"

func TestParseTurnUsage(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-6","usage":{"input_tokens":3,"cache_creation_input_tokens":8652,"cache_read_input_tokens":11352,"output_tokens":122},"content":[{"type":"text","text":"hello"}]}}`
	u := ParseTurnUsage([]byte(line))
	if u == nil {
		t.Fatal("expected non-nil TurnUsage")
	}
	if u.Model != "opus-4-6" {
		t.Errorf("Model = %q, want %q", u.Model, "opus-4-6")
	}
	if u.InputTokens != 3 {
		t.Errorf("InputTokens = %d, want 3", u.InputTokens)
	}
	if u.CacheCreationInputTokens != 8652 {
		t.Errorf("CacheCreationInputTokens = %d, want 8652", u.CacheCreationInputTokens)
	}
	if u.CacheReadInputTokens != 11352 {
		t.Errorf("CacheReadInputTokens = %d, want 11352", u.CacheReadInputTokens)
	}
	if u.OutputTokens != 122 {
		t.Errorf("OutputTokens = %d, want 122", u.OutputTokens)
	}
	if got := u.TotalInputTokens(); got != 20007 {
		t.Errorf("TotalInputTokens() = %d, want 20007", got)
	}
}

func TestParseTurnUsage_UserEntry(t *testing.T) {
	line := `{"type":"user","message":{"role":"user","content":"hello"}}`
	if u := ParseTurnUsage([]byte(line)); u != nil {
		t.Error("expected nil for user entry")
	}
}

func TestParseTurnUsage_NoUsage(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`
	if u := ParseTurnUsage([]byte(line)); u != nil {
		t.Error("expected nil for assistant entry without usage")
	}
}

func TestParseTurnUsage_InvalidJSON(t *testing.T) {
	if u := ParseTurnUsage([]byte(`not json`)); u != nil {
		t.Error("expected nil for invalid JSON")
	}
}

func TestParseTurnUsage_ModelString(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10,"output_tokens":20},"content":[]}}`
	u := ParseTurnUsage([]byte(line))
	if u == nil {
		t.Fatal("expected non-nil")
	}
	if u.Model != "sonnet-4-6" {
		t.Errorf("Model = %q, want %q", u.Model, "sonnet-4-6")
	}
}

func TestFormatUsageStatusLine(t *testing.T) {
	tests := []struct {
		model string
		in    int
		out   int
		want  string
	}{
		{"opus-4-6", 15234, 4521, "opus-4-6 | 15k↓ 4k↑"},
		{"sonnet-4-6", 500, 200, "sonnet-4-6 | 500↓ 200↑"},
		{"opus-4-6", 0, 0, "opus-4-6"},
		{"", 1000, 500, "1k↓ 500↑"},
		{"", 0, 0, ""},
	}
	for _, tt := range tests {
		got := FormatUsageStatusLine(tt.model, tt.in, tt.out)
		if got != tt.want {
			t.Errorf("FormatUsageStatusLine(%q, %d, %d) = %q, want %q",
				tt.model, tt.in, tt.out, got, tt.want)
		}
	}
}

func TestShortenModel(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{map[string]any{"id": "claude-opus-4-6"}, "opus-4-6"},
		{map[string]any{"id": "claude-haiku-4-5-20251001"}, "haiku-4-5"},
		{"claude-sonnet-4-6", "sonnet-4-6"},
		{nil, ""},
	}
	for _, tt := range tests {
		got := shortenModel(tt.input)
		if got != tt.want {
			t.Errorf("shortenModel(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
