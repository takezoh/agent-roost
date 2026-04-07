package claude

import "testing"

func TestParseStatusLine(t *testing.T) {
	input := `{"session_id":"abc-123","cost":{"total_cost_usd":0.1234},"context_window":{"used_percentage":45,"total_input_tokens":15234,"total_output_tokens":4521},"model":{"id":"claude-sonnet-4-6"}}`
	status, err := ParseStatusLine([]byte(input))
	if err != nil {
		t.Fatalf("ParseStatusLine: %v", err)
	}
	if status.SessionID != "abc-123" {
		t.Errorf("SessionID = %q", status.SessionID)
	}
	if status.Cost < 0.12 || status.Cost > 0.13 {
		t.Errorf("Cost = %f, want ~0.1234", status.Cost)
	}
	if status.ContextUsed != 45 {
		t.Errorf("ContextUsed = %d, want 45", status.ContextUsed)
	}
	if status.Model != "sonnet-4-6" {
		t.Errorf("Model = %q, want %q", status.Model, "sonnet-4-6")
	}
	if status.InputTokens != 15234 {
		t.Errorf("InputTokens = %d, want 15234", status.InputTokens)
	}
	if status.OutputTokens != 4521 {
		t.Errorf("OutputTokens = %d, want 4521", status.OutputTokens)
	}
}

func TestFormatStatusLine(t *testing.T) {
	s := StatusData{
		Model:        "opus-4-6",
		ContextUsed:  45,
		Cost:         1.23,
		InputTokens:  15234,
		OutputTokens: 4521,
	}
	got := s.FormatStatusLine()
	want := "opus-4-6 | ctx:45% ████░░░░░░ | $1.23 | 15k↓ 4k↑"
	if got != want {
		t.Errorf("FormatStatusLine() = %q, want %q", got, want)
	}
}

func TestFormatStatusLine_Empty(t *testing.T) {
	s := StatusData{}
	if got := s.FormatStatusLine(); got != "" {
		t.Errorf("FormatStatusLine() = %q, want empty", got)
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
